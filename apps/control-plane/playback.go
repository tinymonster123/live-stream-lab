package main

import (
	"fmt"
	"strings"
	"time"
)

// resolvePlayback owns protocol selection. Callers only consume the descriptor.
//
// The descriptor is an ordered policy: the primary protocol plus fallbacks the
// player may try when the primary cannot start. Ordering lives here so callers
// never learn protocol names; the player still makes the final choice because
// only it knows what the current browser can actually attempt.
func resolvePlayback(
	streamID string,
	latencyClass LatencyClass,
	config ServiceConfig,
	now time.Time,
) (PlaybackDescriptor, error) {
	expiresAt := int(now.Add(time.Hour).UnixMilli())

	switch latencyClass {
	case Low:
		return PlaybackDescriptor{
			Protocol:  Flv,
			Url:       joinURL(config.FLVBase, streamID+".flv"),
			ExpiresAt: expiresAt,
			Fallbacks: []PlaybackDescriptor{{
				Protocol:  Hls,
				Url:       joinURL(config.HLSBase, streamID+".m3u8"),
				ExpiresAt: expiresAt,
				Fallbacks: []PlaybackDescriptor{},
			}},
		}, nil
	case Standard:
		return PlaybackDescriptor{
			Protocol:  Hls,
			Url:       joinURL(config.HLSBase, streamID+".m3u8"),
			ExpiresAt: expiresAt,
			Fallbacks: []PlaybackDescriptor{},
		}, nil
	default:
		return PlaybackDescriptor{}, fmt.Errorf(
			"%w: %q is not implemented until WHEP lands",
			ErrUnsupportedLatency,
			latencyClass,
		)
	}
}

func joinURL(base, path string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}
