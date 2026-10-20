package broadcast

import (
	"errors"
	"testing"
	"time"

	"github.com/tinymonster123/live-stream-lab/apps/control-plane/internal/contracts"
)

func TestResolvePlaybackMapsLatencyClassToProtocols(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	config := testConfig()

	tests := []struct {
		name string
		// latency is the product-level intent the caller states.
		latency contracts.LatencyClass
		// protocol is the primary the player tries first.
		protocol contracts.PlaybackDescriptorProtocol
		url      string
		// fallbacks are tried in order when the primary cannot start.
		fallbacks []contracts.PlaybackDescriptorProtocol
	}{
		{
			name:      "low prefers HTTP-FLV and falls back to HLS",
			latency:   contracts.Low,
			protocol:  contracts.Flv,
			url:       testFLVBase + "/stream-1.flv",
			fallbacks: []contracts.PlaybackDescriptorProtocol{contracts.Hls},
		},
		{
			name:      "standard uses HLS only",
			latency:   contracts.Standard,
			protocol:  contracts.Hls,
			url:       testHLSBase + "/stream-1.m3u8",
			fallbacks: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			descriptor, err := resolvePlayback("stream-1", test.latency, config, now)
			if err != nil {
				t.Fatalf("resolvePlayback: %v", err)
			}

			if descriptor.Protocol != test.protocol {
				t.Fatalf("protocol = %q, want %q", descriptor.Protocol, test.protocol)
			}
			if descriptor.Url != test.url {
				t.Fatalf("url = %q, want %q", descriptor.Url, test.url)
			}
			wantExpiry := int(now.Add(time.Hour).UnixMilli())
			if descriptor.ExpiresAt != wantExpiry {
				t.Fatalf("expiresAt = %d, want %d", descriptor.ExpiresAt, wantExpiry)
			}

			if len(descriptor.Fallbacks) != len(test.fallbacks) {
				t.Fatalf("fallbacks = %+v, want %d", descriptor.Fallbacks, len(test.fallbacks))
			}
			for index, protocol := range test.fallbacks {
				fallback := descriptor.Fallbacks[index]
				if fallback.Protocol != protocol {
					t.Fatalf("fallback[%d].protocol = %q, want %q", index, fallback.Protocol, protocol)
				}
				if fallback.Url != testHLSBase+"/stream-1.m3u8" {
					t.Fatalf("fallback[%d].url = %q", index, fallback.Url)
				}
				if fallback.ExpiresAt != wantExpiry {
					t.Fatalf("fallback[%d].expiresAt = %d, want %d", index, fallback.ExpiresAt, wantExpiry)
				}
			}
		})
	}
}

func TestResolvePlaybackRejectsUltraLowUntilWHEP(t *testing.T) {
	config := testConfig()

	_, err := resolvePlayback("stream-1", contracts.UltraLow, config, time.Now())
	if !errors.Is(err, ErrUnsupportedLatency) {
		t.Fatalf("err = %v, want ErrUnsupportedLatency", err)
	}
}
