package broadcast

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const probeInterval = 200 * time.Millisecond

// IngestProbe owns the media-plane checks the control plane needs:
// media actually flowing, resources actually released, and who is watching.
type IngestProbe interface {
	WaitForPublish(ctx context.Context, streamKey string, timeout time.Duration) error
	EnsureUnpublished(ctx context.Context, streamKey string, timeout time.Duration) error
	ViewerCount(ctx context.Context, streamKey string) (int, error)
}

type SRSProbe struct {
	BaseURL string
	Client  *http.Client
}

func NewSRSProbe(baseURL string) *SRSProbe {
	return &SRSProbe{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Client:  &http.Client{Timeout: 2 * time.Second},
	}
}

func (probe *SRSProbe) WaitForPublish(ctx context.Context, streamKey string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error

	for {
		state, err := probe.streamState(ctx, streamKey)
		switch {
		case err != nil:
			lastErr = err
		case state.active:
			return nil
		}

		if time.Now().After(deadline) {
			if lastErr != nil {
				return fmt.Errorf("stream %q is not publishing: %w", streamKey, lastErr)
			}
			return fmt.Errorf("stream %q is not publishing after %s", streamKey, timeout)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(probeInterval):
		}
	}
}

func (probe *SRSProbe) EnsureUnpublished(ctx context.Context, streamKey string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error

	for {
		state, err := probe.streamState(ctx, streamKey)
		switch {
		case err != nil:
			lastErr = err
		case !state.active:
			return nil
		case state.cid != "":
			if kickErr := probe.kickClient(ctx, state.cid); kickErr != nil {
				lastErr = kickErr
			}
		}

		if time.Now().After(deadline) {
			if lastErr != nil {
				return fmt.Errorf("stream %q is still publishing: %w", streamKey, lastErr)
			}
			return fmt.Errorf("stream %q is still publishing after %s", streamKey, timeout)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(probeInterval):
		}
	}
}

type streamsResponse struct {
	Code    int `json:"code"`
	Streams []struct {
		Name    string `json:"name"`
		Clients int    `json:"clients"`
		Publish struct {
			Active bool   `json:"active"`
			CID    string `json:"cid"`
		} `json:"publish"`
	} `json:"streams"`
}

// streamState is one SRS streams query. SRS reports `clients` as the number of
// players consuming the stream, which is the Broadcast's viewer count.
type streamState struct {
	active  bool
	cid     string
	clients int
}

func (probe *SRSProbe) streamState(ctx context.Context, streamKey string) (streamState, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, probe.BaseURL+"/api/v1/streams/", nil)
	if err != nil {
		return streamState{}, err
	}

	response, err := probe.Client.Do(request)
	if err != nil {
		return streamState{}, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return streamState{}, fmt.Errorf("SRS API returned %d", response.StatusCode)
	}

	var body streamsResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return streamState{}, err
	}
	if body.Code != 0 {
		return streamState{}, fmt.Errorf("SRS API returned code %d", body.Code)
	}

	for _, stream := range body.Streams {
		if stream.Name == streamKey && stream.Publish.Active {
			return streamState{
				active:  true,
				cid:     stream.Publish.CID,
				clients: stream.Clients,
			}, nil
		}
	}
	return streamState{}, nil
}

// ViewerCount returns the number of clients playing the stream. SRS counts the
// publisher among a stream's clients, so an active stream's publisher is
// subtracted. A stream that SRS does not know about has no viewers.
func (probe *SRSProbe) ViewerCount(ctx context.Context, streamKey string) (int, error) {
	state, err := probe.streamState(ctx, streamKey)
	if err != nil {
		return 0, err
	}

	viewers := state.clients
	if state.active && viewers > 0 {
		viewers--
	}
	return viewers, nil
}

func (probe *SRSProbe) kickClient(ctx context.Context, cid string) error {
	endpoint := probe.BaseURL + "/api/v1/clients/" + url.PathEscape(cid)
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}

	response, err := probe.Client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("SRS API returned %d", response.StatusCode)
	}

	var body struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return err
	}
	if body.Code != 0 {
		return fmt.Errorf("SRS API returned code %d", body.Code)
	}
	return nil
}
