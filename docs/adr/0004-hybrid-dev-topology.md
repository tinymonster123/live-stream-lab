# Run infrastructure in Compose; run Go and Python natively

Status: accepted

SRS, Redis, and Caddy run in Docker Compose; the Go control plane and the Python AI worker run natively on macOS. Docker Desktop runs a Linux VM, so a containerized AI worker cannot use Metal/MLX. The hybrid keeps infrastructure isolated while preserving the M4 for Whisper.

## Considered Options

- **All native**: build SRS from source and run Redis/Caddy via brew services; fastest, but more host setup.
- **All Compose**: the AI worker loses Metal/MLX and falls back to CPU whisper.cpp.

## Consequences

- Docker Desktop needs a reachable registry mirror; direct `registry-1.docker.io` times out here.
- SRS in Compose needs `CANDIDATE=<Mac LAN IP>` and a published `8000/udp`, plus `1935`, `8080`, and `1985` as needed.
- Nx starts the native apps; Compose owns infrastructure.
- Prototype evidence lives in `prototype/process-topology/`.
