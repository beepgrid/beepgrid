package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestProfilingRefusesNonLoopbackAndIsDisabledByDefault(t *testing.T) {
	stop, e := startDiagnostics(context.Background(), nil, "")
	if e != nil {
		t.Fatal(e)
	}
	stop()
	for _, address := range []string{"0.0.0.0:6060", "localhost:6060", "[::]:6060", "192.0.2.1:6060", "bad"} {
		if _, e = startDiagnostics(context.Background(), nil, address); e == nil {
			t.Fatal("public diagnostics listener accepted", address)
		}
	}
}

func TestProfilingRefusesForeignHostsOriginsAndMethods(t *testing.T) {
	probe, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	address := probe.Addr().String()
	probe.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop, e := startDiagnostics(ctx, nil, address)
	if e != nil {
		t.Fatal(e)
	}
	defer stop()
	client := &http.Client{Timeout: time.Second}
	for _, fixture := range []struct {
		host, origin, method string
		status               int
	}{
		{address, "", "GET", 200},
		{"attacker.example", "", "GET", 403},
		{"192.0.2.1:80", "", "GET", 403},
		{address, "https://attacker.example", "GET", 403},
		{address, "", "POST", 403},
	} {
		r, e := http.NewRequest(fixture.method, "http://"+address+"/debug/pprof/", nil)
		if e != nil {
			t.Fatal(e)
		}
		r.Host = fixture.host
		r.Header.Set("Origin", fixture.origin)
		res, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != fixture.status {
			t.Fatal(fixture, res.StatusCode)
		}
	}
}
