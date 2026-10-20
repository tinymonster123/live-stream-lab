package broadcast

import (
	"fmt"
	"strings"
	"time"

	"github.com/tinymonster123/live-stream-lab/apps/control-plane/internal/contracts"
)

// resolvePlayback owns protocol selection. Callers only consume the descriptor.
//
// The descriptor is an ordered policy: the primary protocol plus fallbacks the
// player may try when the primary cannot start. Ordering lives here so callers
// never learn protocol names; the player still makes the final choice because
// only it knows what the current browser can actually attempt.
func resolvePlayback(
	streamID string,
	latencyClass contracts.LatencyClass,
	config Config,
	now time.Time,
) (contracts.PlaybackDescriptor, error) {
	expiresAt := int(now.Add(time.Hour).UnixMilli())

	switch latencyClass {
	case contracts.Low:
		return contracts.PlaybackDescriptor{
			Protocol:  contracts.Flv,
			Url:       joinURL(config.FLVBase, streamID+".flv"),
			ExpiresAt: expiresAt,
			Fallbacks: []contracts.PlaybackDescriptor{{
				Protocol:  contracts.Hls,
				Url:       joinURL(config.HLSBase, streamID+".m3u8"),
				ExpiresAt: expiresAt,
				Fallbacks: []contracts.PlaybackDescriptor{},
			}},
		}, nil
	case contracts.Standard:
		return contracts.PlaybackDescriptor{
			Protocol:  contracts.Hls,
			Url:       joinURL(config.HLSBase, streamID+".m3u8"),
			ExpiresAt: expiresAt,
			Fallbacks: []contracts.PlaybackDescriptor{},
		}, nil
	default:
		return contracts.PlaybackDescriptor{}, fmt.Errorf(
			"%w: %q is not implemented until WHEP lands",
			ErrUnsupportedLatency,
			latencyClass,
		)
	}
}

func joinURL(base, path string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}
