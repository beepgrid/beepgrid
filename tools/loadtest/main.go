// Command loadtest runs opt-in, isolated local server qualification. Never point
// this command at an existing deployment: it creates its own database and server.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/notif-dev/notif/sdk/go/notif"
)

type job struct {
	Sequence int    `json:"sequence"`
	Sent     int64  `json:"sent"`
	Padding  string `json:"padding"`
	Digest   string `json:"digest"`
}
type observation struct {
	Sequence     int     `json:"sequence"`
	MessageID    string  `json:"message_id"`
	Instance     string  `json:"instance"`
	ReceiptMS    float64 `json:"receipt_ms"`
	CompletionMS float64 `json:"completion_ms"`
}
type transport struct {
	base    http.RoundTripper
	half    time.Duration
	drop    bool
	dropped bool
	mu      sync.Mutex
	wire    int64
	calls   int
	errors  int
}
type countedBody struct {
	io.ReadCloser
	t *transport
}

func (b countedBody) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	b.t.mu.Lock()
	b.t.wire += int64(n)
	b.t.mu.Unlock()
	return n, e
}
func (t *transport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.half > 0 {
		select {
		case <-time.After(t.half):
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	}
	r = r.Clone(r.Context())
	r.Header = r.Header.Clone()
	r.Header.Set("Accept-Encoding", "gzip")
	res, e := t.base.RoundTrip(r)
	t.mu.Lock()
	t.calls++
	if e != nil {
		t.errors++
	}
	t.mu.Unlock()
	if e != nil {
		return nil, e
	}
	if t.half > 0 {
		select {
		case <-time.After(t.half):
		case <-r.Context().Done():
			res.Body.Close()
			return nil, r.Context().Err()
		}
	}
	t.mu.Lock()
	drop := t.drop && !t.dropped && r.Method == "POST" && r.URL.Path == "/api/v1/messages"
	if drop {
		t.dropped = true
	}
	t.mu.Unlock()
	res.Body = countedBody{res.Body, t}
	if drop {
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		res.Body = io.NopCloser(strings.NewReader("{"))
		return res, nil
	}
	if res.Header.Get("Content-Encoding") == "gzip" {
		raw := res.Body
		z, e := gzip.NewReader(raw)
		if e != nil {
			raw.Close()
			return nil, e
		}
		res.Body = decodedBody{Reader: z, raw: raw}
		res.Header.Del("Content-Encoding")
		res.Header.Del("Content-Length")
		res.ContentLength = -1
		res.Uncompressed = true
	}
	return res, nil
}

type decodedBody struct {
	*gzip.Reader
	raw io.Closer
}

func (b decodedBody) Close() error { b.Reader.Close(); return b.raw.Close() }
func baseTransport() http.RoundTripper {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableCompression = true
	return t
}
func token() string {
	b := make([]byte, 24)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func writeJSON(path string, value any) {
	raw, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		panic(e)
	}
	if e = os.WriteFile(path, append(raw, '\n'), 0600); e != nil {
		panic(e)
	}
}
func main() {
	if len(os.Args) > 1 && os.Args[1] == "worker" {
		if e := worker(); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		return
	}
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func worker() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	half, _ := strconv.Atoi(os.Getenv("LOAD_RTT_MS"))
	think, _ := strconv.Atoi(os.Getenv("LOAD_THINK_MS"))
	tr := &transport{base: baseTransport(), half: time.Duration(half) * time.Millisecond / 2, drop: os.Getenv("LOAD_DROP") == "1"}
	c := notif.New(os.Getenv("NOTIF_URL"), os.Getenv("NOTIF_TOKEN"))
	c.HTTP.Transport = tr
	path := os.Getenv("LOAD_REPORT")
	out, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer out.Close()
	if _, e = out.WriteString("{\"ready\":true}\n"); e != nil {
		return e
	}
	e = c.Run(ctx, func(ctx context.Context, d notif.Delivery) error {
		if d.Payload.Message == nil {
			return fmt.Errorf("unexpected payload")
		}
		var j job
		if e := json.Unmarshal([]byte(d.Payload.Message.Body), &j); e != nil {
			return e
		}
		sum := sha256.Sum256([]byte(j.Padding))
		if hex.EncodeToString(sum[:]) != j.Digest {
			return fmt.Errorf("payload hash mismatch")
		}
		received := float64(time.Now().UnixNano()-j.Sent) / 1e6
		if think > 0 {
			select {
			case <-time.After(time.Duration(think) * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		result, e := c.Send(ctx, notif.SendInput{ConversationID: d.Payload.Message.ConversationID, Body: fmt.Sprintf("completed/%d/%s", j.Sequence, d.Payload.Message.ID)}, "load/reply/"+d.Payload.Message.ID)
		if e != nil {
			return e
		}
		if len(result.Messages) != 1 {
			return fmt.Errorf("reply result count")
		}
		observation := observation{j.Sequence, d.Payload.Message.ID, result.Messages[0].InstanceID, received, float64(time.Now().UnixNano()-j.Sent) / 1e6}
		raw, _ := json.Marshal(observation)
		_, e = out.Write(append(raw, '\n'))
		if e == nil {
			e = out.Sync()
		}
		return e
	})
	tr.mu.Lock()
	summary := map[string]any{"wire_bytes": tr.wire, "calls": tr.calls, "transport_errors": tr.errors, "injected_lost_response": tr.dropped}
	tr.mu.Unlock()
	raw, _ := json.Marshal(summary)
	out.Write(append(raw, '\n'))
	if errors.Is(e, context.Canceled) {
		return nil
	}
	return e
}

type config struct {
	server, label, out, mode                             string
	agents, rounds, payload, backlog, idle, rtt, readers int
	think, drop, profile                                 bool
}

func call(c *notif.Client, method, path string, in, out any) error {
	return c.Do(context.Background(), method, path, in, out, "")
}
func metric(url string) (map[string]float64, error) {
	res, e := (&http.Client{Timeout: 2 * time.Second}).Get(url)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close()
	var v map[string]float64
	e = json.NewDecoder(res.Body).Decode(&v)
	return v, e
}
func quantile(v []float64, p float64) float64 {
	if len(v) == 0 {
		return 0
	}
	sort.Float64s(v)
	i := int(math.Ceil(p*float64(len(v)))) - 1
	return v[max(0, min(i, len(v)-1))]
}
func stats(v []float64) map[string]float64 {
	return map[string]float64{"p50": quantile(v, .5), "p95": quantile(v, .95), "p99": quantile(v, .99), "max": quantile(v, 1)}
}
func cgroup() map[string]int64 {
	out, e := exec.Command("docker", "exec", "notif-postgres-1", "sh", "-c", "cat /sys/fs/cgroup/cpu.stat /sys/fs/cgroup/memory.current /sys/fs/cgroup/memory.stat").Output()
	if e != nil {
		return nil
	}
	result := map[string]int64{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 1 {
			result["memory_current"], _ = strconv.ParseInt(f[0], 10, 64)
		}
		if len(f) == 2 {
			result[f[0]], _ = strconv.ParseInt(f[1], 10, 64)
		}
	}
	return result
}
func delta(a, b map[string]float64) map[string]float64 {
	out := map[string]float64{}
	for k, v := range b {
		out[k] = v - a[k]
	}
	return out
}
func run() (err error) {
	var cfg config
	flag.StringVar(&cfg.server, "server", ".local/notif", "server binary")
	flag.StringVar(&cfg.label, "label", "screen", "variant label")
	flag.StringVar(&cfg.out, "out", ".local/server-perf/screen", "owned output directory")
	flag.StringVar(&cfg.mode, "mode", "burst", "burst or idle")
	flag.IntVar(&cfg.agents, "agents", 25, "1 to 25 independent workers")
	flag.IntVar(&cfg.rounds, "rounds", 6, "broadcast rounds")
	flag.IntVar(&cfg.payload, "payload", 1024, "padding bytes per job")
	flag.IntVar(&cfg.backlog, "backlog", 0, "completed delivery rows per agent")
	flag.IntVar(&cfg.idle, "idle-seconds", 10, "idle phase duration")
	flag.IntVar(&cfg.readers, "readers", 1, "0 to 5 dashboard-style API/SSE readers")
	flag.IntVar(&cfg.rtt, "rtt-ms", 0, "simulated application RTT per worker call")
	flag.BoolVar(&cfg.think, "think", false, "varied 0 to 200 ms worker delay")
	flag.BoolVar(&cfg.drop, "drop", false, "lose the first committed reply response per worker")
	flag.BoolVar(&cfg.profile, "profile", false, "capture CPU/heap profiles")
	flag.Parse()
	if cfg.readers < 0 || cfg.readers > 5 {
		return fmt.Errorf("reader limit exceeded")
	}
	if cfg.agents < 1 || cfg.agents > 25 || cfg.rounds < 1 || cfg.rounds > 100 || cfg.payload < 0 || cfg.payload > 28000 || cfg.backlog < 0 || cfg.backlog > 10000 || cfg.idle < 0 || cfg.idle > 60 {
		return fmt.Errorf("load bounds exceeded")
	}
	if cfg.mode != "burst" && cfg.mode != "idle" {
		return fmt.Errorf("unknown mode")
	}
	cfg.server, err = filepath.Abs(cfg.server)
	if err != nil {
		return err
	}
	cfg.out, err = filepath.Abs(cfg.out)
	if err != nil {
		return err
	}
	if entries, e := os.ReadDir(cfg.out); e == nil && len(entries) > 0 {
		return fmt.Errorf("output directory must be new or empty")
	}
	if err = os.MkdirAll(cfg.out, 0700); err != nil {
		return err
	}
	for _, address := range []string{"127.0.0.1:4014", "127.0.0.1:4015"} {
		l, e := net.Listen("tcp", address)
		if e != nil {
			return fmt.Errorf("benchmark port busy: %s", address)
		}
		l.Close()
	}
	report := map[string]any{"result": "running", "mode": cfg.mode, "idle_seconds": cfg.idle, "variant": cfg.label, "go": runtime.Version(), "agents": cfg.agents, "rounds": cfg.rounds, "payload_padding_bytes": cfg.payload, "completed_history_rows": cfg.agents * cfg.backlog, "readers": cfg.readers, "rtt_ms": cfg.rtt, "think": cfg.think, "lost_responses": cfg.drop}
	defer func() {
		if err != nil {
			report["result"] = "failed"
			report["error"] = err.Error()
		}
		writeJSON(filepath.Join(cfg.out, "report.json"), report)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	database := os.Getenv("NOTIF_TEST_DATABASE_URL")
	if database == "" {
		database = "postgres://notif:notif@localhost:5499/notif?sslmode=disable"
	}
	admin, e := pgx.Connect(ctx, database)
	if e != nil {
		return e
	}
	defer admin.Close(context.Background())
	name := "notif_load_" + token()[:12]
	if _, e = admin.Exec(ctx, "CREATE DATABASE "+name); e != nil {
		return e
	}
	defer func() { admin.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)") }()
	dbconfig, e := pgx.ParseConfig(database)
	if e != nil {
		return e
	}
	dbconfig.Database = name
	// Keep base URL credentials only in child environment, never command-line args or reports.
	dbURL, e := urlParse(database, name)
	if e != nil {
		return e
	}
	ownerToken, key := token()+token(), token()+token()
	env := append(os.Environ(), "NOTIF_DATABASE_URL="+dbURL, "NOTIF_SCHEMA=notif", "NOTIF_WORKSPACE=load", "NOTIF_ENVIRONMENT=development", "NOTIF_BOOTSTRAP_TOKEN="+ownerToken, "NOTIF_SECRET_KEY="+key[:64], "NOTIF_ADDR=127.0.0.1:4014", "NOTIF_PUBLIC_URL=http://localhost:4014", "NOTIF_PROFILE_ADDR=127.0.0.1:4015", "NOTIF_ATTACHMENTS_DIR="+filepath.Join(cfg.out, "attachments"))
	migrate := exec.CommandContext(ctx, cfg.server, "migrate")
	migrate.Env = env
	if out, e := migrate.CombinedOutput(); e != nil {
		return fmt.Errorf("migration failed: %v %s", e, out)
	}
	log, e := os.OpenFile(filepath.Join(cfg.out, "server.log"), os.O_CREATE|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer log.Close()
	server := exec.Command(cfg.server, "serve", "--dev")
	server.Env = env
	server.Stdout = log
	server.Stderr = log
	if e = server.Start(); e != nil {
		return e
	}
	defer func() { server.Process.Signal(syscall.SIGTERM); server.Wait() }()
	for i := 0; i < 100; i++ {
		res, e := http.Get("http://localhost:4014/readyz")
		if e == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				break
			}
		}
		if i == 99 {
			return fmt.Errorf("server readiness timeout")
		}
		time.Sleep(50 * time.Millisecond)
	}
	db, e := pgx.ConnectConfig(ctx, dbconfig)
	if e != nil {
		return e
	}
	defer db.Close(context.Background())
	c := notif.New("http://localhost:4014", ownerToken)
	ownerTransport := &transport{base: baseTransport()}
	c.HTTP.Transport = ownerTransport
	var me struct {
		Person notif.Person `json:"person"`
	}
	if e = call(c, "GET", "/api/v1/me", nil, &me); e != nil {
		return e
	}
	var role struct {
		ID string `json:"id"`
	}
	rules := []map[string]string{}
	for _, action := range []string{"view", "message", "ask"} {
		rules = append(rules, map[string]string{"action": action, "target_kind": "person", "target_id": me.Person.ID})
	}
	if e = call(c, "POST", "/api/v1/roles", map[string]any{"name": "Load worker scope", "rules": rules}, &role); e != nil {
		return e
	}
	members := []string{}
	workers := []*exec.Cmd{}
	files := []string{}
	sessions := []notif.Session{}
	executable, _ := os.Executable()
	defer func() {
		for _, w := range workers {
			w.Process.Signal(syscall.SIGTERM)
		}
		for _, w := range workers {
			w.Wait()
		}
	}()
	for i := 0; i < cfg.agents; i++ {
		_, session, e := c.Enroll(ctx, notif.EnrollInput{Agent: fmt.Sprint("load/", i), Name: fmt.Sprint("Load worker ", i), Capabilities: []string{"message", "request"}})
		if e != nil {
			return e
		}
		sessions = append(sessions, session)
		members = append(members, session.Agent.ID)
		if e = call(c, "POST", "/api/v1/role-bindings", map[string]any{"role_id": role.ID, "subject_kind": "principal", "subject_id": session.Agent.ID}, nil); e != nil {
			return e
		}
		file := filepath.Join(cfg.out, fmt.Sprintf("worker-%02d.jsonl", i))
		files = append(files, file)
		think := 0
		if cfg.think {
			think = []int{0, 10, 40, 100, 200}[i%5]
		}
		w := exec.Command(executable, "worker")
		w.Env = append(os.Environ(), "NOTIF_URL=http://localhost:4014", "NOTIF_TOKEN="+session.Token, "LOAD_REPORT="+file, "LOAD_RTT_MS="+strconv.Itoa(cfg.rtt), "LOAD_THINK_MS="+strconv.Itoa(think), "LOAD_DROP="+map[bool]string{true: "1", false: "0"}[cfg.drop])
		w.Stderr = log
		if e = w.Start(); e != nil {
			return e
		}
		workers = append(workers, w)
	}
	var group struct {
		ID string `json:"id"`
	}
	if e = call(c, "POST", "/api/v1/groups", map[string]any{"name": "Load fleet", "members": members}, &group); e != nil {
		return e
	}
	if cfg.backlog > 0 {
		// Archived receipt history is fixture setup, not a throughput/enrollment claim.
		_, e = db.Exec(ctx, `INSERT INTO notif.deliveries(id,scope,operation_id,recipient_id,payload,expires_at,state,created_at) SELECT 'history_'||p.id||'_'||n,'load/development','old_'||n,p.id,'{}',now()+interval '1 day','received',now()-interval '1 day' FROM notif.principals p CROSS JOIN generate_series(1,$1::int) n WHERE p.kind='agent'`, cfg.backlog)
		if e != nil {
			return e
		}
		if _, e = db.Exec(ctx, `ANALYZE notif.deliveries`); e != nil {
			return e
		}
	}
	// Warm up idle long polls and keep enrollment/fixture construction separate.
	time.Sleep(time.Second)
	before, e := metric("http://127.0.0.1:4015/debug/metrics")
	if e != nil {
		return e
	}
	pgBefore := cgroup()
	dbBefore, e := databaseStats(ctx, db)
	if e != nil {
		return e
	}
	ownerTransport.mu.Lock()
	ownerTransport.wire = 0
	ownerTransport.calls = 0
	ownerTransport.mu.Unlock()
	browserCtx, stopBrowsers := context.WithCancel(ctx)
	defer stopBrowsers()
	var browserWG sync.WaitGroup
	browserTransports := []*transport{}
	for i := 0; i < cfg.readers; i++ {
		tr := &transport{base: baseTransport()}
		browserTransports = append(browserTransports, tr)
		browserWG.Add(2)
		go func() { defer browserWG.Done(); browserPoll(browserCtx, c.BaseURL, ownerToken, tr) }()
		go func() { defer browserWG.Done(); browserStream(browserCtx, c.BaseURL, ownerToken, tr) }()
	}
	phaseStart := time.Now()
	metrics := []map[string]float64{}
	var metricsMu sync.Mutex
	sampling, stopSample := context.WithCancel(ctx)
	defer stopSample()
	sampleDone := make(chan struct{})
	go func() {
		defer close(sampleDone)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-sampling.Done():
				return
			case <-ticker.C:
				if m, e := metric("http://127.0.0.1:4015/debug/metrics"); e == nil {
					metricsMu.Lock()
					metrics = append(metrics, m)
					metricsMu.Unlock()
				}
			}
		}
	}()
	profileDone := make(chan struct{})
	if cfg.profile {
		go func() {
			defer close(profileDone)
			res, e := http.Get("http://127.0.0.1:4015/debug/pprof/profile?seconds=10")
			if e == nil {
				defer res.Body.Close()
				f, e := os.Create(filepath.Join(cfg.out, "cpu.pprof"))
				if e == nil {
					io.Copy(f, res.Body)
					f.Close()
				}
			}
		}()
	} else {
		close(profileDone)
	}
	latencies := []float64{}
	if cfg.mode == "idle" {
		time.Sleep(time.Duration(cfg.idle) * time.Second)
	} else {
		for round := 0; round < cfg.rounds; round++ {
			padding := strings.Repeat("x", cfg.payload)
			sum := sha256.Sum256([]byte(padding))
			body, _ := json.Marshal(job{round, time.Now().UnixNano(), padding, hex.EncodeToString(sum[:])})
			started := time.Now()
			r, e := c.Send(ctx, notif.SendInput{Target: notif.Target{Kind: "group", ID: group.ID}, Body: string(body), Title: "Load fixture"}, fmt.Sprint("round/", round))
			if e != nil {
				return e
			}
			if len(r.Messages) != cfg.agents {
				return fmt.Errorf("broadcast recipient mismatch")
			}
			latencies = append(latencies, float64(time.Since(started))/float64(time.Millisecond))
		}
		deadline := time.Now().Add(90 * time.Second)
		for {
			count := 0
			for _, file := range files {
				obs, _ := observations(file)
				count += len(obs)
			}
			var acknowledged int
			if e = db.QueryRow(ctx, `SELECT count(*) FROM notif.deliveries WHERE state='received' AND operation_id NOT LIKE 'old_%' AND recipient_id=ANY($1::text[])`, members).Scan(&acknowledged); e != nil {
				return e
			}
			if count == cfg.agents*cfg.rounds && acknowledged == count {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("drain timeout: %d observations, %d acknowledgements", count, acknowledged)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	duration := time.Since(phaseStart).Seconds()
	stopBrowsers()
	browserWG.Wait()
	stopSample()
	<-sampleDone
	after, e := metric("http://127.0.0.1:4015/debug/metrics")
	if e != nil {
		return e
	}
	pgAfter := cgroup()
	dbAfter, e := databaseStats(ctx, db)
	if e != nil {
		return e
	}
	// Stop only the processes created by this run before reading final worker totals.
	for _, w := range workers {
		w.Process.Signal(syscall.SIGTERM)
	}
	for _, w := range workers {
		w.Wait()
	}
	workers = nil
	receipt, completion := []float64{}, []float64{}
	wire, calls := ownerTransport.wire, ownerTransport.calls
	for i, file := range files {
		obs, summary := observations(file)
		if cfg.mode != "idle" && len(obs) != cfg.rounds {
			return fmt.Errorf("worker %d result count %d", i, len(obs))
		}
		seen := map[string]bool{}
		for _, v := range obs {
			if seen[v.MessageID] || v.Instance != sessions[i].InstanceID {
				return fmt.Errorf("duplicate result or false instance")
			}
			seen[v.MessageID] = true
			receipt = append(receipt, v.ReceiptMS)
			completion = append(completion, v.CompletionMS)
		}
		wire += int64(summary["wire_bytes"])
		calls += int(summary["calls"])
	}
	var messages, replies, acked int
	if e = db.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE body LIKE 'completed/%') FROM notif.messages`).Scan(&messages, &replies); e != nil {
		return e
	}
	if e = db.QueryRow(ctx, `SELECT count(*) FROM notif.deliveries WHERE state='received' AND operation_id NOT LIKE 'old_%'`).Scan(&acked); e != nil {
		return e
	}
	expected := 0
	if cfg.mode != "idle" {
		expected = cfg.agents * cfg.rounds
	}
	if messages != expected*2 || replies != expected || acked != expected {
		return fmt.Errorf("canonical mismatch: messages=%d replies=%d receipts=%d expected=%d", messages, replies, acked, expected)
	}
	<-profileDone
	if cfg.profile {
		res, e := http.Get("http://127.0.0.1:4015/debug/pprof/heap")
		if e == nil {
			defer res.Body.Close()
			f, e := os.Create(filepath.Join(cfg.out, "heap.pprof"))
			if e == nil {
				io.Copy(f, res.Body)
				f.Close()
			}
		}
	}
	browserBytes, browserCalls := int64(0), 0
	for _, tr := range browserTransports {
		tr.mu.Lock()
		browserBytes += tr.wire
		browserCalls += tr.calls
		if tr.errors != 0 {
			tr.mu.Unlock()
			return fmt.Errorf("dashboard reader errors: %d", tr.errors)
		}
		tr.mu.Unlock()
	}
	report["browser_response_body_bytes"] = browserBytes
	report["browser_http_calls"] = browserCalls
	report["result"] = "passed"
	report["phase_seconds"] = duration
	report["work_per_second"] = float64(expected) / duration
	report["broadcast_api_ms"] = stats(latencies)
	report["receipt_ms"] = stats(receipt)
	report["completion_ms"] = stats(completion)
	report["go_before"] = before
	report["go_after"] = after
	report["go_delta"] = delta(before, after)
	report["pg_cgroup_before"] = pgBefore
	report["pg_cgroup_after"] = pgAfter
	report["pg_database_delta"] = delta(dbBefore, dbAfter)
	report["response_body_bytes"] = wire
	report["client_http_calls"] = calls
	report["canonical_messages"] = messages
	report["canonical_replies"] = replies
	report["acknowledged"] = acked
	writeJSON(filepath.Join(cfg.out, "samples.json"), metrics)
	fmt.Printf("%s: %d agents, %d replies in %.3fs, completion p95 %.1fms, CPU %.3fs, allocated %.1fMiB\n", cfg.label, cfg.agents, replies, duration, quantile(completion, .95), after["cpu_seconds"]-before["cpu_seconds"], (after["total_alloc_bytes"]-before["total_alloc_bytes"])/(1<<20))
	return nil
}
func observations(path string) ([]observation, map[string]float64) {
	raw, _ := os.ReadFile(path)
	out := []observation{}
	summary := map[string]float64{}
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var v map[string]json.RawMessage
		if json.Unmarshal(line, &v) != nil {
			continue
		}
		if v["message_id"] != nil {
			var x observation
			if json.Unmarshal(line, &x) == nil {
				out = append(out, x)
			}
		}
		if v["wire_bytes"] != nil {
			json.Unmarshal(line, &summary)
		}
	}
	return out, summary
}
func databaseStats(ctx context.Context, c *pgx.Conn) (map[string]float64, error) {
	if _, e := c.Exec(ctx, `SELECT pg_stat_clear_snapshot()`); e != nil {
		return nil, e
	}
	var commits, rollbacks, hits, read, returned, fetched, inserted, updated float64
	e := c.QueryRow(ctx, `SELECT xact_commit,xact_rollback,blks_hit,blks_read,tup_returned,tup_fetched,tup_inserted,tup_updated FROM pg_stat_database WHERE datname=current_database()`).Scan(&commits, &rollbacks, &hits, &read, &returned, &fetched, &inserted, &updated)
	return map[string]float64{"commits": commits, "rollbacks": rollbacks, "buffer_hits": hits, "blocks_read": read, "tuples_returned": returned, "tuples_fetched": fetched, "tuples_inserted": inserted, "tuples_updated": updated}, e
}

func urlParse(raw, name string) (string, error) {
	u, e := url.Parse(raw)
	if e != nil {
		return "", e
	}
	u.Path = "/" + name
	return u.String(), nil
}

func browserPoll(ctx context.Context, base, token string, tr *transport) {
	c := notif.New(base, token)
	c.HTTP.Transport = tr
	paths := []string{"me", "people", "agents", "services", "requests", "conversations", "groups", "activity", "roles", "connections", "role-bindings", "destinations"}
	for ctx.Err() == nil {
		var wg sync.WaitGroup
		for _, path := range paths {
			wg.Add(1)
			go func(path string) {
				defer wg.Done()
				var value any
				if e := c.Do(ctx, "GET", "/api/v1/"+path, nil, &value, ""); e != nil && ctx.Err() == nil {
					tr.mu.Lock()
					tr.errors++
					tr.mu.Unlock()
				}
			}(path)
		}
		wg.Wait()
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}
func browserStream(ctx context.Context, base, token string, tr *transport) {
	client := &http.Client{Transport: tr}
	cursor := ""
	for ctx.Err() == nil {
		req, e := http.NewRequestWithContext(ctx, "GET", base+"/api/v1/events?stream=1", nil)
		if e != nil {
			return
		}
		req.Header.Set("Authorization", "Bearer "+token)
		if cursor != "" {
			req.Header.Set("Last-Event-ID", cursor)
		}
		res, e := client.Do(req)
		if e == nil {
			if res.StatusCode != 200 {
				tr.mu.Lock()
				tr.errors++
				tr.mu.Unlock()
			}
			scanner := bufio.NewScanner(res.Body)
			scanner.Buffer(make([]byte, 4096), 262144)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "id: ") {
					cursor = strings.TrimPrefix(line, "id: ")
				}
			}
			if scanner.Err() != nil && ctx.Err() == nil {
				tr.mu.Lock()
				tr.errors++
				tr.mu.Unlock()
			}
			res.Body.Close()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}
