package main

import (
	"fmt"
	"strings"
	"time"
)

// resolvePlayback owns protocol selection. Callers only consume the descriptor.
// T3 implements the low latency class; HLS and WHEP arrive with T4 and T6.
func resolvePlayback(streamID string, latencyClass LatencyClass, flvBase string, now time.Time) (PlaybackDescriptor, error) {
	if latencyClass != Low {
		return PlaybackDescriptor{}, fmt.Errorf(
			"%w: %q (only low/HTTP-FLV is implemented)",
			ErrUnsupportedLatency,
			latencyClass,
		)
	}

	return PlaybackDescriptor{
		Protocol: Flv,
		Url:      joinURL(flvBase, streamID+".flv"),
		// URLs are not signed yet; the expiry is advisory until Playback owns tokens.
		ExpiresAt: int(now.Add(time.Hour).UnixMilli()),
	}, nil
}

func joinURL(base, path string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}
