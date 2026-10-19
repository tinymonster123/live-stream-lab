import { readFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const CONTRACTS_DIR = dirname(fileURLToPath(import.meta.url));
const SCHEMA_FILES = ["ai-job.json", "transcript.json", "summary.json"];
const HTTP_METHODS = new Set(["get", "put", "post", "delete", "options", "head", "patch", "trace"]);

function readJson(path) {
  return JSON.parse(readFileSync(path, "utf8"));
}

function collectRefs(node, refs = []) {
  if (Array.isArray(node)) {
    for (const item of node) {
      collectRefs(item, refs);
    }
    return refs;
  }

  if (node && typeof node === "object") {
    for (const [key, value] of Object.entries(node)) {
      if (key === "$ref" && typeof value === "string") {
        refs.push(value);
      } else {
        collectRefs(value, refs);
      }
    }
  }

  return refs;
}

function resolvePointer(document, pointer) {
  if (!pointer.startsWith("#/")) {
    return undefined;
  }

  let current = document;
  for (const rawToken of pointer.slice(2).split("/")) {
    const token = rawToken.replaceAll("~1", "/").replaceAll("~0", "~");
    if (current === null || typeof current !== "object" || !(token in current)) {
      return undefined;
    }
    current = current[token];
  }

  return current;
}

function validateOpenApi(openapi, errors) {
  if (!/^3\.1\.\d+$/.test(String(openapi.openapi ?? ""))) {
    errors.push("openapi.json: openapi must be a 3.1.x version");
  }

  if (typeof openapi.info?.title !== "string" || openapi.info.title.length === 0) {
    errors.push("openapi.json: info.title is required");
  }

  if (!openapi.paths || typeof openapi.paths !== "object") {
    errors.push("openapi.json: paths is required");
    return;
  }

  const operationIds = new Set();
  for (const [path, pathItem] of Object.entries(openapi.paths)) {
    for (const [method, operation] of Object.entries(pathItem)) {
      if (!HTTP_METHODS.has(method)) {
        continue;
      }

      const label = `${method.toUpperCase()} ${path}`;
      if (typeof operation.operationId !== "string" || operation.operationId.length === 0) {
        errors.push(`openapi.json: ${label} has no operationId`);
      } else if (operationIds.has(operation.operationId)) {
        errors.push(`openapi.json: duplicate operationId ${operation.operationId}`);
      } else {
        operationIds.add(operation.operationId);
      }

      if (!operation.responses || Object.keys(operation.responses).length === 0) {
        errors.push(`openapi.json: ${label} has no responses`);
      }
    }
  }

  for (const ref of new Set(collectRefs(openapi))) {
    if (ref.startsWith("#/") && resolvePointer(openapi, ref) === undefined) {
      errors.push(`openapi.json: unresolved $ref ${ref}`);
    }
  }

  if (typeof openapi["x-websocket"]?.path !== "string") {
    errors.push("openapi.json: x-websocket.path is required");
  }
}

function validateJsonSchema(file, schema, errors) {
  if (schema.$schema !== "https://json-schema.org/draft/2020-12/schema") {
    errors.push(`${file}: $schema must be JSON Schema 2020-12`);
  }

  if (schema.type !== "object") {
    errors.push(`${file}: top-level type must be object`);
  }

  if (schema.additionalProperties !== false) {
    errors.push(`${file}: additionalProperties must be false`);
  }

  const properties = schema.properties ?? {};
  for (const key of schema.required ?? []) {
    if (!(key in properties)) {
      errors.push(`${file}: required property ${key} is not declared in properties`);
    }
  }
}

export function validateContracts(rootDir = CONTRACTS_DIR) {
  const errors = [];

  let openapi;
  try {
    openapi = readJson(join(rootDir, "openapi.json"));
    validateOpenApi(openapi, errors);
  } catch (error) {
    errors.push(`openapi.json: ${error.message}`);
  }

  for (const file of SCHEMA_FILES) {
    try {
      validateJsonSchema(file, readJson(join(rootDir, "schemas", file)), errors);
    } catch (error) {
      errors.push(`${file}: ${error.message}`);
    }
  }

  return {
    ok: errors.length === 0,
    errors,
    documents: ["openapi.json", ...SCHEMA_FILES.map((file) => `schemas/${file}`)],
  };
}

const invokedDirectly =
  process.argv[1] !== undefined && resolve(process.argv[1]) === fileURLToPath(import.meta.url);

if (invokedDirectly) {
  const result = validateContracts();
  if (!result.ok) {
    console.error(result.errors.join("\n"));
    process.exit(1);
  }
  console.log(`contracts ok: ${result.documents.join(", ")}`);
}
