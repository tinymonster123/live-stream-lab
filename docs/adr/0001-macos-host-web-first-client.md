# Use a macOS PC host and a web first client

Status: accepted (2026-10-19)

We build the media plane on the macOS host where FFmpeg, SRS, and capture devices live, and make the browser the first client. This gives the fastest loop: localhost is a secure context, the player libraries are mature, and there is no signing or emulator friction. Mobile clients are viewers after the media plane is stable.

## Considered Options

| Client | Iteration | Capture | Playback | Setup friction |
|---|---|---|---|---|
| Web | fastest | browser permissions, secure context | HLS/FLV/WebRTC | lowest |
| PC native | fast, more UI work | full device control | native/ffplay | medium |
| iOS | slow | native AVFoundation | HLS/WebRTC, no FLV | signing, store |
| Android | medium | camera/mic | MSE/WebRTC | emulator camera |

## Consequences

- A browser on `localhost` is the first client; LAN phones need HTTPS.
- iOS Safari plays HLS or WebRTC, not HTTP-FLV.
- The macOS host runs the media plane; the browser is only a client.
