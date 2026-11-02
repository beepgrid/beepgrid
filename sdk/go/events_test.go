package beepgrid

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func eventFrame(n int) string {
	return fmt.Sprintf("id: %d\r\ndata: {\"sequence\":%d,\"kind\":\"message.created\",\"data\":{\"body\":\"héllo\"}}\r\n\r\n", n, n)
}
func TestEventsReconnectCompleteFramesAndHandlerCheckpoint(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("missing credential")
		}
		if calls == 1 {
			fmt.Fprint(w, eventFrame(1)+"id: 2\nevent: cursor\ndata: {}\n\nid: 3\ndata: {\"partial\":")
		} else {
			if r.Header.Get("Last-Event-ID") != "2" || r.URL.Query().Get("after") != "2" {
				t.Error("wrong resume", r.URL)
			}
			fmt.Fprint(w, eventFrame(1)+eventFrame(3))
		}
	}))
	defer server.Close()
	done := errors.New("handler checkpoint stop")
	var ids []int64
	err := New(server.URL, "fixture").StreamEvents(context.Background(), EventOptions{}, func(event Event) error {
		ids = append(ids, event.Sequence)
		if event.Sequence == 3 {
			return done
		}
		return nil
	})
	if err != done || calls != 2 || fmt.Sprint(ids) != "[1 3]" {
		t.Fatal(ids, calls, err)
	}
	cursor := int64(0)
	if err = consumeEvents(strings.NewReader(eventFrame(1)), &cursor, func(Event) error { return done }); err != done || cursor != 0 {
		t.Fatal("advanced before handling", cursor, err)
	}
}
func TestEventsRejectRevocationInvalidFramesAndCancellation(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(403) }))
	defer server.Close()
	err := New(server.URL, "fixture").StreamEvents(context.Background(), EventOptions{}, func(Event) error { return nil })
	var api *APIError
	if !errors.As(err, &api) || api.Status != 403 || calls != 1 {
		t.Fatal(err, calls)
	}
	cursor := int64(0)
	for _, raw := range []string{"id: 2\ndata: {\"sequence\":3,\"kind\":\"message.created\"}\n\n", "data: " + strings.Repeat("x", 8<<20)} {
		if err = consumeEvents(strings.NewReader(raw), &cursor, func(Event) error { return nil }); !errors.Is(err, errEventFrame) {
			t.Fatal("accepted invalid frame", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err = New(server.URL, "fixture").StreamEvents(ctx, EventOptions{}, func(Event) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
