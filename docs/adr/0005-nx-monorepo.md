# Use Nx to orchestrate a three-language monorepo

Status: accepted

The repo contains a TypeScript web app, a Go control plane, and a Python AI worker. Nx provides the project graph, affected builds, and caching; native toolchains — pnpm, Go modules, and uv — remain the runtime. `@nx-go/nx-go` 4.1.1 supports Nx 20–23; `@nxlv/python` 23.2.1 supports Nx >=22. Use `nx:run-commands` for Go and Python targets rather than deep plugin coupling.

## Considered Options

- **Plain pnpm workspace + `go work` + uv**: less orchestration, more ad-hoc scripts.
- **Bazel**: polyglot, but too heavy for this repo.

## Consequences

- Nx is JavaScript-centric; Go and Python build logic stays in their native tools.
- Layout: `apps/web`, `apps/control-plane`, `apps/ai-worker`, `libs/contracts`, `infra/srs`, `infra/caddy`, `docs/`.
