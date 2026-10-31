package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/pprof"
	"runtime"
	"syscall"
	"time"

	"github.com/beepgrid/beepgrid/internal/engine"
)

// Profiling is opt-in, on a separate literal loopback listener. Never mount it on
// the public router: heap and execution profiles can contain application data.
func startDiagnostics(ctx context.Context, s *engine.Store, address string) (func(), error) {
	if address == "" {
		return func() {}, nil
	}
	host, _, e := net.SplitHostPort(address)
	if e != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return nil, engine.Bad("NOTIF_PROFILE_ADDR must use a literal loopback IP and port")
	}
	listener, e := net.Listen("tcp", address)
	if e != nil {
		return nil, e
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	mux.HandleFunc("GET /debug/metrics", func(w http.ResponseWriter, r *http.Request) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		var usage syscall.Rusage
		_ = syscall.Getrusage(syscall.RUSAGE_SELF, &usage)
		cpu := float64(usage.Utime.Sec+usage.Stime.Sec) + float64(usage.Utime.Usec+usage.Stime.Usec)/1e6
		rss := usage.Maxrss
		if runtime.GOOS != "darwin" {
			rss *= 1024
		}
		pool := s.Pool.Stat()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"cpu_seconds": cpu, "peak_rss_bytes": rss, "heap_alloc_bytes": m.HeapAlloc, "heap_inuse_bytes": m.HeapInuse, "heap_retained_bytes": m.HeapSys - m.HeapReleased, "total_alloc_bytes": m.TotalAlloc, "gc_count": m.NumGC, "gc_pause_ns": m.PauseTotalNs, "goroutines": runtime.NumGoroutine(), "pool_acquires": pool.AcquireCount(), "pool_wait_ns": pool.AcquireDuration().Nanoseconds(), "pool_connections": pool.TotalConns(), "pool_acquired": pool.AcquiredConns()})
	})
	server := &http.Server{ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 30 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, _, e := net.SplitHostPort(r.Host)
		if e != nil || net.ParseIP(h) == nil || !net.ParseIP(h).IsLoopback() || r.Header.Get("Origin") != "" || r.Method != "GET" {
			http.Error(w, "Local profiling requests only", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})}
	go server.Serve(listener)
	stop := func() { server.Close() }
	go func() { <-ctx.Done(); stop() }()
	return stop, nil
}
