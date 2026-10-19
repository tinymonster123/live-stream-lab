# Decisions

Canonical records live in `docs/adr/`. This file is the index and the open-questions log.

| ADR | Decision |
|---|---|
| [0001](adr/0001-macos-host-web-first-client.md) | macOS PC host + Web first client |
| [0002](adr/0002-srs-sfu-go-roster.md) | SRS WHIP/WHEP SFU; Go owns rooms and subscriptions |
| [0003](adr/0003-per-participant-recording-and-local-ai.md) | Per-participant FFmpeg recording + local MLX Whisper + Redis Streams |
| [0004](adr/0004-hybrid-dev-topology.md) | Compose infrastructure + native Go/Python apps |
| [0005](adr/0005-nx-monorepo.md) | Nx orchestrates the three-language monorepo |
| [0006](adr/0006-openapi-json-schema-contracts.md) | OpenAPI 3.1 + JSON Schema contracts |
| [0007](adr/0007-latency-as-product-intent.md) | Latency is a product-level class, not a protocol name |
| [0008](adr/0008-pin-srs-and-ffmpeg.md) | Pin SRS 6.0-r2 + Homebrew FFmpeg for the first slices |

## Open

- Screen share: after M3.
- Real-time captions: after the post-meeting AI pipeline.
- Public internet / TURN: later; LAN only for now.
- Recording retention: 7 days or 50GB, first to delete; confirm during implementation.
- Observability: not yet decided.
- CI baseline: `.github/workflows/ci.yml` (T2).
- Real SRS Compose image and health check: verify with `ossrs/srs:6`.

## Milestones

- **M1**: one-way Broadcast. FFmpeg/OBS → SRS → browser FLV/HLS, LAN, 10 minutes stable.
- **M2**: 1:1 Meeting + per-participant Recording + post-meeting AI. Two browsers over HTTPS, bidirectional audio/video, mute, rejoin, LAN <500ms.
- **M3**: 2–6 participant Room + multi-track Recording, reusing the M2 recorder.
