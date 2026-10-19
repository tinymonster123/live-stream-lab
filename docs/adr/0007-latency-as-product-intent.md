# Model latency as a product-level class

Status: accepted

`BroadcastSpec.latency` is `ultra-low`, `low`, or `standard`; the implementation maps it to protocols. `ultra-low` uses WHIP/WHEP, `low` uses RTMP + HTTP-FLV, and `standard` uses HLS. Callers state intent, not protocol names.

## Considered Options

- **Callers choose protocols**: leaks implementation details and makes a protocol change touch every call site.

## Consequences

- Adding a protocol does not change callers.
- The mapping lives in `StreamSession` / `RoomSession`, not in the UI.
