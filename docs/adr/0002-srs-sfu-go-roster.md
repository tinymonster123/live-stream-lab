# SRS is the SFU; the Go control plane owns rooms and subscriptions

Status: accepted

We use SRS WHIP/WHEP as the WebRTC SFU. Each Participant publishes its Track as a Stream; the Go control plane owns the Roster and returns each Participant the list of Streams to subscribe to. Room events go over WebSocket. SRS is stream-based and provides neither room membership nor subscription logic, so the control plane must.

## Considered Options

- **Custom SDP signaling + SRS RTC API**: duplicates what WHIP/WHEP already do.
- **LiveKit or mediasoup**: a second SFU contradicts the SRS decision.

## Consequences

- Join and subscribe pass through the Go control plane; it is on the critical path.
- Subscriptions can be spread across multiple SRS instances later without changing the interface.
- SRS needs `CANDIDATE=<LAN IP>` and a published `8000/udp`; WHIP on non-localhost needs HTTPS.
