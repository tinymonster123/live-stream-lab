import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { describe, it } from "node:test";
import { fileURLToPath } from "node:url";
import { validateContracts } from "./validate.mjs";

const CONTRACTS_DIR = dirname(fileURLToPath(import.meta.url));
const openapi = JSON.parse(readFileSync(join(CONTRACTS_DIR, "openapi.json"), "utf8"));
const readSchema = (file) => JSON.parse(readFileSync(join(CONTRACTS_DIR, "schemas", file), "utf8"));

describe("contracts", () => {
  it("passes the repo's structural checks", () => {
    const result = validateContracts();

    assert.deepEqual(result.errors, []);
    assert.equal(result.ok, true);
  });

  it("declares the initial HTTP operations", () => {
    const operationIds = Object.values(openapi.paths).flatMap((pathItem) =>
      Object.values(pathItem).map((operation) => operation.operationId),
    );

    for (const operationId of [
      "getHealth",
      "createBroadcast",
      "startBroadcast",
      "stopBroadcast",
      "createMeeting",
      "joinRoom",
      "leaveRoom",
      "getRoomStatus",
      "listRecordings",
      "getRecording",
      "listArtifacts",
    ]) {
      assert.ok(operationIds.includes(operationId), `missing ${operationId}`);
    }
  });

  it("declares the WebSocket signaling channel", () => {
    assert.equal(openapi["x-websocket"].path, "/api/v1/rooms/{roomCode}/ws");
    assert.ok(openapi.components.schemas.WsEvent.oneOf.length > 0);
  });

  it("maps Latency Class to protocols through PlaybackDescriptor", () => {
    const { PlaybackDescriptor, LatencyClass } = openapi.components.schemas;

    assert.deepEqual(LatencyClass.enum, ["ultra-low", "low", "standard"]);
    assert.deepEqual(PlaybackDescriptor.properties.protocol.enum, ["hls", "flv", "whep"]);
    assert.equal(PlaybackDescriptor.properties.expiresAt.type, "integer");
  });

  it("defines a Recording as tracks plus a room event timeline", () => {
    const { Recording, RoomEvent } = openapi.components.schemas;

    assert.ok(Recording.required.includes("timeline"));
    assert.equal(Recording.properties.timeline.items.$ref, "#/components/schemas/RoomEvent");
    assert.ok(RoomEvent.required.includes("at"));
    assert.ok(RoomEvent.required.includes("type"));
  });

  it("describes an AI Job", () => {
    const schema = readSchema("ai-job.json");

    assert.deepEqual(schema.required, [
      "jobId",
      "meetingId",
      "recordingId",
      "recordingTracks",
      "language",
      "model",
      "createdAt",
    ]);
    assert.deepEqual(schema.properties.recordingTracks.items.required, ["participantId", "path"]);
  });

  it("describes a Transcript with speaker-attributed segments", () => {
    const schema = readSchema("transcript.json");

    assert.deepEqual(schema.required, ["language", "model", "segments"]);
    assert.deepEqual(schema.properties.segments.items.required, [
      "startMs",
      "endMs",
      "participantId",
      "text",
    ]);
  });

  it("describes a Summary with key points", () => {
    const schema = readSchema("summary.json");

    assert.deepEqual(schema.required, ["text", "keyPoints"]);
    assert.equal(schema.properties.keyPoints.items.type, "string");
  });
});
