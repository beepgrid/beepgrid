package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfiguredMatcherProtocolBudgetsAndCancellation(t *testing.T) {
	s, _ := testStore(t)
	s.AllowLocal = true
	ctx := context.Background()
	var active, maxActive atomic.Int32
	entered := make(chan struct{}, 3)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-judge-token" {
			t.Error("missing matcher authentication")
		}
		if r.URL.Path == "/large" {
			w.Write([]byte(strings.Repeat("x", 32769)))
			return
		}
		var input map[string]any
		if e := json.NewDecoder(r.Body).Decode(&input); e != nil {
			t.Error(e)
		}
		raw := string(JSON(input))
		if strings.Contains(raw, "PRIVATE ANSWER") {
			t.Error("answer contents unnecessarily exposed to judge")
		}
		n := active.Add(1)
		defer active.Add(-1)
		for {
			old := maxActive.Load()
			if n <= old || maxActive.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"suggestions":[{"answer_id":"answer-fixture","reason":"Same explicit scope"}]}`))
	}))
	defer server.Close()
	if e := s.ConfigureMatcher(ctx, server.URL, "fixture-judge-token"); e != nil {
		t.Fatal(e)
	}
	answers := []SharedAnswer{{ID: "answer-fixture", Question: "Tax convention?", Key: "tax/v1", Answer: "PRIVATE ANSWER"}}
	done := make(chan error, 2)
	for _, id := range []string{"one", "two"} {
		go func(id string) {
			v, e := s.Matcher(ctx, Request{RequesterID: id, Title: "Tax?", SharedKey: "tax/v1"}, answers)
			if e == nil && (len(v) != 1 || v[0].AnswerID != "answer-fixture") {
				t.Error(v)
			}
			done <- e
		}(id)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("matcher did not start")
		}
	}
	bounded, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if _, e := s.Matcher(bounded, Request{RequesterID: "third", Title: "Tax?"}, answers); e == nil {
		t.Fatal("queued matcher ignored cancellation")
	}
	if maxActive.Load() != 2 {
		t.Fatal("concurrency budget", maxActive.Load())
	}
	close(release)
	for i := 0; i < 2; i++ {
		if e := <-done; e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.Matcher(ctx, Request{RequesterID: "one", Title: "Repeated"}, answers); e == nil || !strings.Contains(e.Error(), "rate limited") {
		t.Fatal("requester rate limit", e)
	}
	if _, e := s.Matcher(ctx, Request{RequesterID: "large-input", Body: strings.Repeat("x", 32769)}, answers); e == nil {
		t.Fatal("unbounded matcher input")
	}
	if e := s.ConfigureMatcher(ctx, server.URL+"/large", "fixture-judge-token"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Matcher(ctx, Request{RequesterID: "large-output", Title: "Tax?"}, answers); e == nil || !strings.Contains(e.Error(), "too large") {
		t.Fatal("unbounded matcher response", e)
	}
}
