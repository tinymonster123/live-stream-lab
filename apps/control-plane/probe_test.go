package main

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
