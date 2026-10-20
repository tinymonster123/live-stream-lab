package broadcast

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSRSProbeFindsActiveStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, `{"code":0,"streams":[{"name":"b1","publish":{"active":true}}]}`)
	}))
	defer server.Close()

	probe := NewSRSProbe(server.URL)
	if err := probe.WaitForPublish(context.Background(), "b1", time.Second); err != nil {
		t.Fatalf("WaitForPublish: %v", err)
	}
}

func TestSRSProbeTimesOutWithoutPublish(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, `{"code":0,"streams":[{"name":"b1","publish":{"active":false}}]}`)
	}))
	defer server.Close()

	probe := NewSRSProbe(server.URL)
	err := probe.WaitForPublish(context.Background(), "b1", 50*time.Millisecond)

	if err == nil || !strings.Contains(err.Error(), "not publishing") {
		t.Fatalf("err = %v, want a not-publishing error", err)
	}
}

func TestSRSProbeReportsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	probe := NewSRSProbe(server.URL)
	err := probe.WaitForPublish(context.Background(), "b1", 50*time.Millisecond)

	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v, want an SRS API error", err)
	}
}

func TestSRSProbeReportsViewerCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, `{"code":0,"streams":[
			{"name":"other","clients":99,"publish":{"active":true}},
			{"name":"b1","clients":4,"publish":{"active":true}}
		]}`)
	}))
	defer server.Close()

	probe := NewSRSProbe(server.URL)
	count, err := probe.ViewerCount(context.Background(), "b1")
	if err != nil {
		t.Fatalf("ViewerCount: %v", err)
	}
	if count != 3 {
		t.Fatalf("viewers = %d, want 3 (SRS counts the publisher too)", count)
	}
}

func TestSRSProbeViewerCountIgnoresThePublisher(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, `{"code":0,"streams":[{"name":"b1","clients":1,"publish":{"active":true}}]}`)
	}))
	defer server.Close()

	probe := NewSRSProbe(server.URL)
	count, err := probe.ViewerCount(context.Background(), "b1")
	if err != nil {
		t.Fatalf("ViewerCount: %v", err)
	}
	if count != 0 {
		t.Fatalf("viewers = %d, want 0 with only the publisher connected", count)
	}
}

func TestSRSProbeViewerCountIsZeroForMissingStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(writer, `{"code":0,"streams":[]}`)
	}))
	defer server.Close()

	probe := NewSRSProbe(server.URL)
	count, err := probe.ViewerCount(context.Background(), "b1")
	if err != nil {
		t.Fatalf("ViewerCount: %v", err)
	}
	if count != 0 {
		t.Fatalf("viewers = %d, want 0", count)
	}
}

func TestSRSProbeViewerCountReportsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	probe := NewSRSProbe(server.URL)
	if _, err := probe.ViewerCount(context.Background(), "b1"); err == nil {
		t.Fatal("ViewerCount succeeded, want an SRS API error")
	}
}

func TestSRSProbeKicksPublisherOnStop(t *testing.T) {
	var mu sync.Mutex
	active := true
	deletes := 0

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		if request.Method == http.MethodDelete {
			deletes++
			active = false
			fmt.Fprint(writer, `{"code":0}`)
			return
		}
		fmt.Fprintf(
			writer,
			`{"code":0,"streams":[{"name":"b1","publish":{"active":%t,"cid":"c1"}}]}`,
			active,
		)
	}))
	defer server.Close()

	probe := NewSRSProbe(server.URL)
	if err := probe.EnsureUnpublished(context.Background(), "b1", time.Second); err != nil {
		t.Fatalf("EnsureUnpublished: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if deletes != 1 {
		t.Fatalf("publisher kicks = %d, want 1", deletes)
	}
}
