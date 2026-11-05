package engine

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

type Connection struct {
	Version          int64             `json:"version"`
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Kind             string            `json:"kind"`
	Config           map[string]string `json:"config"`
	Enabled          bool              `json:"enabled"`
	SecretConfigured bool              `json:"secret_configured"`
	Secret           string            `json:"secret,omitempty"`
}
type Destination struct {
	Version      int64  `json:"version"`
	Mode         string `json:"mode"`
	Priority     int    `json:"priority"`
	ID           string `json:"id"`
	ConnectionID string `json:"connection_id"`
	PrincipalID  string `json:"principal_id"`
	Address      string `json:"address"`
}

func (s *Store) seal(plain []byte) ([]byte, error) {
	if len(s.SecretKey) != 32 {
		return nil, Bad("A 32-byte encryption key is required for provider credentials.")
	}
	block, e := aes.NewCipher(s.SecretKey)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	return g.Seal(nonce, nonce, plain, []byte("beepgrid/connection/v1")), nil
}
func (s *Store) unseal(raw []byte) ([]byte, error) {
	block, e := aes.NewCipher(s.SecretKey)
	if e != nil {
		return nil, e
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	if len(raw) < g.NonceSize() {
		return nil, fmt.Errorf("invalid encrypted secret")
	}
	return g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], []byte("beepgrid/connection/v1"))
}

// AES-GCM envelopes contain a 12-byte nonce and 16-byte tag. The encrypted
// empty value for unauthenticated SMTP is not a configured password.
func (s *Store) Connections(ctx context.Context, a Actor) ([]Connection, error) {

	rows, e := s.Pool.Query(ctx, `SELECT id,name,kind,config,enabled,octet_length(secret)>28,version FROM connections WHERE scope=$1 ORDER BY name`, a.Scope)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Connection{}
	for rows.Next() {
		var c Connection
		var raw []byte
		if e = rows.Scan(&c.ID, &c.Name, &c.Kind, &raw, &c.Enabled, &c.SecretConfigured, &c.Version); e != nil {
			return nil, e
		}
		_ = json.Unmarshal(raw, &c.Config)
		out = append(out, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	visible := []Connection{}
	for _, c := range out {
		manage := s.mayManageConnection(ctx, s.Pool, a, c.ID)
		var own bool
		if e = s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM destinations WHERE scope=$1 AND connection_id=$2 AND principal_id=$3)`, a.Scope, c.ID, a.ID).Scan(&own); e != nil {
			return nil, e
		}
		if manage || own || s.allowed(ctx, s.Pool, a, "view", Target{"connection", c.ID}, "", "") {
			if !manage {
				c.Config = map[string]string{}
			}
			visible = append(visible, c)
		}
	}
	return visible, nil
}
func (s *Store) SaveConnection(ctx context.Context, a Actor, c Connection) (Connection, error) {
	if len(c.Secret) > 8192 || (c.Kind != "email" && c.Secret != "" && len(c.Secret) < 16) {
		return c, Bad("Provider credentials exceed bounds; signing secrets need at least 16 characters.")
	}
	if !ValidText(c.Name, 100) || len(JSON(c.Config)) > 8192 {
		return c, Bad("A name and bounded configuration are required.")
	}
	if c.Kind != "webhook" && c.Kind != "slack" && c.Kind != "email" && c.Kind != "telegram" && c.Kind != "discord" {
		return c, Bad("Unsupported connection type.")
	}
	if c.Kind == "webhook" {
		if _, e := s.validURL(ctx, c.Config["url"]); e != nil {
			return c, e
		}
	}
	if c.Kind == "slack" && c.Config["team_id"] == "" {
		return c, Bad("Slack team_id is required.")
	}
	if c.Kind == "discord" {
		if !validDiscordConfig(c.Config) {
			return c, Bad("Discord requires an immutable application_id and 32-byte hexadecimal public_key.")
		}
		if c.Secret != "" {
			if _, err := parseDiscordSecret(c.Secret); err != nil {
				return c, err
			}
		}
	}
	if c.Kind == "telegram" {
		if !telegramID(c.Config["bot_id"]) {
			return c, Bad("Telegram requires an immutable positive bot_id.")
		}
		if c.Secret != "" {
			if _, err := parseTelegramSecret(c.Secret, c.Config["bot_id"]); err != nil {
				return c, err
			}
		}
	}
	if c.Kind == "email" {
		if _, e := mail.ParseAddress(c.Config["from"]); e != nil {
			return c, Bad("A valid sender address is required.")
		}
		if _, _, e := net.SplitHostPort(c.Config["smtp_addr"]); e != nil {
			return c, Bad("smtp_addr must include host and port.")
		}
	}
	e := s.tx(ctx, a, func(tx pgx.Tx) error {
		if !s.mayManageConnection(ctx, tx, a, c.ID) {
			return Denied()
		}
		for _, field := range []string{"inbound_sender_id", "default_agent_id"} {
			if id := c.Config[field]; id != "" {
				p, e := principal(ctx, tx, a.Scope, id)
				if e != nil {
					return e
				}
				if !a.Admin && !s.managesIdentity(ctx, tx, a, p) {
					return Denied()
				}
				if field == "inbound_sender_id" {
					rules, e := roleRules(ctx, tx, Actor{Principal: p, Scope: a.Scope})
					if e != nil || !s.rulesWithin(ctx, tx, a, rules, p.ID) {
						return Denied()
					}
				}
			}
		}
		if e := checkRevision(ctx, tx, a.Scope, "connections", c.ID, c.Version); e != nil {
			return e
		}
		if c.ID == "" {
			if c.Kind != "email" && len(c.Secret) < 16 {
				return Bad("A provider token or signing secret of at least 16 characters is required.")
			}
			if c.Kind == "email" && c.Config["username"] != "" && c.Secret == "" {
				return Bad("SMTP password is required when a username is configured.")
			}
			c.ID = ID("chn")
		}
		var encrypted []byte
		var e error
		if c.Secret != "" {
			encrypted, e = s.seal([]byte(c.Secret))
		} else {
			e = tx.QueryRow(ctx, `SELECT secret FROM connections WHERE scope=$1 AND id=$2`, a.Scope, c.ID).Scan(&encrypted)
			if e == pgx.ErrNoRows && c.Kind == "email" && c.Config["username"] == "" {
				// A relay without authentication has no password; encrypt the empty value consistently.
				encrypted, e = s.seal(nil)
			}
		}
		if e != nil {
			return e
		}

		if c.Kind == "email" && c.Config["username"] != "" {
			password, err := s.unseal(encrypted)
			if err != nil || len(password) == 0 {
				return Bad("SMTP password is required when a username is configured.")
			}
		}
		if c.Kind == "discord" {
			plain, err := s.unseal(encrypted)
			if err != nil {
				return err
			}
			if _, err = parseDiscordSecret(string(plain)); err != nil {
				return err
			}
			var oldApp string
			err = tx.QueryRow(ctx, `SELECT config->>'application_id' FROM connections WHERE scope=$1 AND id=$2`, a.Scope, c.ID).Scan(&oldApp)
			if err != nil && err != pgx.ErrNoRows {
				return err
			}
			if err == nil && oldApp != c.Config["application_id"] {
				return Conflict("Create a new installation for a different Discord application.")
			}
		}
		if c.Kind == "telegram" {
			plain, err := s.unseal(encrypted)
			if err != nil {
				return err
			}
			if _, err = parseTelegramSecret(string(plain), c.Config["bot_id"]); err != nil {
				return err
			}
			var oldConfig, oldSecret []byte
			var oldEnabled bool
			err = tx.QueryRow(ctx, `SELECT config,secret,enabled FROM connections WHERE scope=$1 AND id=$2`, a.Scope, c.ID).Scan(&oldConfig, &oldSecret, &oldEnabled)
			if err != nil && err != pgx.ErrNoRows {
				return err
			}
			if err == nil {
				var config map[string]string
				if err = json.Unmarshal(oldConfig, &config); err != nil {
					return err
				}
				if config["bot_id"] != c.Config["bot_id"] {
					return Conflict("Create a new installation for a different Telegram bot.")
				}
				if !oldEnabled && c.Enabled {
					previous, err := s.unseal(oldSecret)
					if err != nil {
						return err
					}
					oldAuth, _ := parseTelegramSecret(string(previous), c.Config["bot_id"])
					newAuth, _ := parseTelegramSecret(string(plain), c.Config["bot_id"])
					if oldAuth.WebhookSecret == newAuth.WebhookSecret {
						return Conflict("Rotate the Telegram webhook secret before reconnecting a disabled installation.")
					}
				}
			}
		}
		tag, e := tx.Exec(ctx, `INSERT INTO connections(id,scope,name,kind,config,secret,enabled) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,config=EXCLUDED.config,secret=EXCLUDED.secret,enabled=EXCLUDED.enabled,version=connections.version+1 WHERE connections.scope=EXCLUDED.scope AND connections.kind=EXCLUDED.kind`, c.ID, a.Scope, c.Name, c.Kind, JSON(c.Config), encrypted, c.Enabled)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return Denied()
		}
		if e = tx.QueryRow(ctx, `SELECT version,octet_length(secret)>28 FROM connections WHERE scope=$1 AND id=$2`, a.Scope, c.ID).Scan(&c.Version, &c.SecretConfigured); e != nil {
			return e
		}
		return event(ctx, tx, a, "connection.saved", "", a.ID, map[string]any{"id": c.ID, "kind": c.Kind})
	})
	c.Secret = ""
	if e != nil {
		c.SecretConfigured = false
	}
	return c, e
}
func (s *Store) SaveDestination(ctx context.Context, a Actor, d Destination) (Destination, error) {
	if d.Mode == "" {
		d.Mode = "primary"
	}
	if d.Mode != "primary" && d.Mode != "fallback" && d.Mode != "copy" {
		return d, Bad("Destination mode must be primary, fallback or copy.")
	}
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		if !s.mayManageDestination(ctx, tx, a, d.ConnectionID, d.PrincipalID) {
			return Denied()
		}
		if e := checkRevision(ctx, tx, a.Scope, "destinations", d.ID, d.Version); e != nil {
			return e
		}
		var kind string
		if e := tx.QueryRow(ctx, `SELECT kind FROM connections WHERE scope=$1 AND id=$2`, a.Scope, d.ConnectionID).Scan(&kind); e != nil {
			return e
		}
		if !ValidText(d.Address, 512) || strings.ContainsAny(d.Address, "\r\n") {
			return Bad("A valid destination is required.")
		}
		if kind == "email" {
			if _, e := mail.ParseAddress(d.Address); e != nil {
				return Bad("Invalid email destination.")
			}
		}
		if kind == "telegram" && !s.telegramDestination(ctx, tx, a.Scope, d.ConnectionID, d.PrincipalID, d.Address) {
			return Bad("Telegram destinations must match the Person's currently linked private user ID and this bot.")
		}
		if kind == "discord" && !s.discordDestination(ctx, tx, a.Scope, d.ConnectionID, d.PrincipalID, d.Address) {
			return Bad("Discord destinations must match the Person's currently linked private user ID and this application.")
		}
		if d.Mode == "primary" && !a.Admin {
			var previous string
			e := tx.QueryRow(ctx, `SELECT connection_id FROM destinations WHERE scope=$1 AND principal_id=$2 AND mode='primary'`, a.Scope, d.PrincipalID).Scan(&previous)
			if e != nil && e != pgx.ErrNoRows {
				return e
			}
			if e == nil && !s.mayManageDestination(ctx, tx, a, previous, d.PrincipalID) {
				return Denied()
			}
		}
		if d.Mode == "primary" {
			if _, e := tx.Exec(ctx, `UPDATE destinations SET mode='fallback',version=version+1 WHERE scope=$1 AND principal_id=$2 AND mode='primary' AND id<>$3`, a.Scope, d.PrincipalID, d.ID); e != nil {
				return e
			}
		}
		if d.ID == "" {
			d.ID = ID("dst")
		}
		tag, e := tx.Exec(ctx, `INSERT INTO destinations(id,scope,connection_id,principal_id,address,mode,priority) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(id) DO UPDATE SET address=EXCLUDED.address,mode=EXCLUDED.mode,priority=EXCLUDED.priority,version=destinations.version+1 WHERE destinations.scope=EXCLUDED.scope AND destinations.principal_id=EXCLUDED.principal_id AND destinations.connection_id=EXCLUDED.connection_id`, d.ID, a.Scope, d.ConnectionID, d.PrincipalID, d.Address, d.Mode, d.Priority)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return Denied()
		}
		if e = tx.QueryRow(ctx, `SELECT version FROM destinations WHERE scope=$1 AND id=$2`, a.Scope, d.ID).Scan(&d.Version); e != nil {
			return e
		}
		return event(ctx, tx, a, "destination.created", "", d.PrincipalID, d)
	})
	return d, err
}
func (s *Store) Destinations(ctx context.Context, a Actor) ([]Destination, error) {

	rows, e := s.Pool.Query(ctx, `SELECT id,connection_id,principal_id,address,mode,priority,version FROM destinations WHERE scope=$1`, a.Scope)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Destination{}
	for rows.Next() {
		var d Destination
		if e = rows.Scan(&d.ID, &d.ConnectionID, &d.PrincipalID, &d.Address, &d.Mode, &d.Priority, &d.Version); e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	visible := []Destination{}
	for _, d := range out {
		if a.Admin || d.PrincipalID == a.ID || s.mayManageDestination(ctx, s.Pool, a, d.ConnectionID, d.PrincipalID) || s.allowed(ctx, s.Pool, a, "view", Target{"destination", d.PrincipalID}, "", "") {
			visible = append(visible, d)
		}
	}
	return visible, nil
}
func (s *Store) LinkSlackAccount(ctx context.Context, a Actor, personID, userID, teamID string) error {
	return s.tx(ctx, a, func(tx pgx.Tx) error {
		if !s.mayLinkAccount(ctx, tx, a, personID) {
			return Denied()
		}
		if !ValidText(userID, 100) || !ValidText(teamID, 100) {
			return Bad("Slack immutable user and team IDs are required.")
		}
		var exists bool
		_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM principals WHERE scope=$1 AND metadata->>'slack_user_id'=$2 AND metadata->>'slack_team_id'=$3 AND id<>$4)`, a.Scope, userID, teamID, personID).Scan(&exists)
		if exists {
			return Conflict("This Slack account is already linked.")
		}
		tag, e := tx.Exec(ctx, `UPDATE principals SET metadata=metadata||$3::jsonb WHERE scope=$1 AND id=$2 AND kind='person'`, a.Scope, personID, JSON(map[string]string{"slack_user_id": userID, "slack_team_id": teamID}))
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return Missing()
		}
		return event(ctx, tx, a, "person.account_linked", "", personID, map[string]any{"provider": "slack", "verified_by": "workspace_admin"})
	})
}
func (s *Store) validURL(ctx context.Context, raw string) (*url.URL, error) {
	return validProviderURL(ctx, raw, s.AllowLocal)
}
func validProviderURL(ctx context.Context, raw string, allowLocal bool) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && !(allowLocal && u.Scheme == "http")) {
		return nil, Bad("An HTTPS provider URL is required.")
	}
	ips, e := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if e != nil || len(ips) == 0 {
		return nil, Bad("Provider host could not be resolved.")
	}
	for _, ip := range ips {
		if !allowLocal && (ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() || ip.IP.IsUnspecified() || ip.IP.IsMulticast()) {
			return nil, Bad("Private provider addresses are disabled.")
		}
	}
	return u, nil
}
func (s *Store) dial(ctx context.Context, network, address string) (net.Conn, error) {
	return dialProvider(ctx, network, address, s.AllowLocal)
}
func dialProvider(ctx context.Context, network, address string, allowLocal bool) (net.Conn, error) {
	host, port, e := net.SplitHostPort(address)
	if e != nil {
		return nil, e
	}
	ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
	if e != nil {
		return nil, e
	}
	for _, ip := range ips {
		if !allowLocal && (ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() || ip.IP.IsUnspecified() || ip.IP.IsMulticast()) {
			return nil, fmt.Errorf("private provider address denied")
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no provider address")
	}
	return dialProviderIPs(ctx, network, port, ips)
}

// All addresses must be validated before calling this helper. Keep the checked
// addresses pinned during failover, rather than resolving the hostname again.
// A refused/unroutable first A/AAAA record must not discard a reachable peer.
func dialProviderIPs(ctx context.Context, network, port string, ips []net.IPAddr) (net.Conn, error) {
	var err error
	for _, ip := range ips {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var conn net.Conn
		host := ip.String()
		conn, err = (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(host, port))
		if err == nil {
			return conn, nil
		}
	}
	if err == nil {
		err = fmt.Errorf("no provider address")
	}
	return nil, err
}
func Signature(secret, timestamp string, body []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte("v1:" + timestamp + ":"))
	h.Write(body)
	return "v1=" + hex.EncodeToString(h.Sum(nil))
}
func VerifySignature(secret, timestamp, signature string, body []byte, now time.Time) bool {
	n, e := strconv.ParseInt(timestamp, 10, 64)
	if e != nil || now.Sub(time.Unix(n, 0)) > 5*time.Minute || time.Unix(n, 0).Sub(now) > time.Minute {
		return false
	}
	return hmac.Equal([]byte(Signature(secret, timestamp, body)), []byte(signature))
}
func VerifySlack(secret, timestamp, signature string, body []byte, now time.Time) bool {
	n, e := strconv.ParseInt(timestamp, 10, 64)
	if e != nil || now.Sub(time.Unix(n, 0)) > 5*time.Minute || time.Unix(n, 0).Sub(now) > time.Minute {
		return false
	}
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte("v0:" + timestamp + ":"))
	h.Write(body)
	return hmac.Equal([]byte("v0="+hex.EncodeToString(h.Sum(nil))), []byte(signature))
}

type DispatchResult struct {
	State, Detail string
	Retry         bool
	RetryAfter    time.Duration
	Receipt       map[string]string
}

func (s *Store) newProviderTransport() *http.Transport {
	return &http.Transport{
		DialContext:         s.dial,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: 5 * time.Second,
		IdleConnTimeout:     60 * time.Second,
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 2,
		MaxConnsPerHost:     8,
	}
}

// DispatchOnce preserves the one-delivery API used by callers and tests.
func (s *Store) DispatchOnce(ctx context.Context, publicURL string) error {
	_, e := s.dispatchOne(ctx, publicURL)
	return e
}

// DispatchBatch bounds independent provider sends to 16 workers. A destination
// lane is leased in PostgreSQL, so replicas cannot race Slack thread creation
// or overtake an in-flight send to the same provider address.
func (s *Store) DispatchBatch(ctx context.Context, publicURL string, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		return 0, Bad("Dispatch batch must be between 1 and 100.")
	}
	if err := s.prepareDispatch(ctx); err != nil {
		return 0, err
	}
	ready, release := s.WatchProviders()
	defer func() { release() }()
	// Keep the idle path to one claim, without launching a worker pool.
	first, worked, err := s.claimProvider(ctx)
	if err != nil || !worked {
		return 0, err
	}
	parallel := min(16, limit)
	jobs := make(chan *providerJob, parallel)
	finished := make(chan error, parallel)
	var wg sync.WaitGroup
	for range parallel {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				finished <- s.finishProvider(ctx, publicURL, job)
			}
		}()
	}
	defer func() { close(jobs); wg.Wait() }()
	n, active := 1, 0
	if first != nil {
		jobs <- first
		active++
	}
	var firstErr error
	tick := time.NewTicker(300 * time.Millisecond)
	defer tick.Stop()
	complete := func(err error) {
		active--
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	for {
		// One scheduler claims work, so idle lanes do not create a query storm.
		// Workers remain available while sends are in flight: a transient workspace
		// lock or a late producer cannot collapse the pool down to one slow sender.
		for firstErr == nil && n < limit && active < parallel {
			next, worked, err := s.claimProvider(ctx)
			if err != nil {
				firstErr = err
				break
			}
			if !worked {
				break
			}
			n++
			if next != nil {
				jobs <- next
				active++
			}
		}
		if active == 0 {
			return n, firstErr
		}
		if firstErr != nil || n == limit || active == parallel {
			complete(<-finished)
			continue
		}
		select {
		case err := <-finished:
			complete(err)
		case <-ready:
			release()
			ready, release = s.WatchProviders()
		case <-tick.C: // Recover a missed hint or a temporarily locked workspace.
		}
	}
}

type providerJob struct {
	scope      string
	connection Connection
	delivery   Delivery
}

func (s *Store) prepareDispatch(ctx context.Context) error {
	if err := s.promoteQuestions(ctx); err != nil {
		return err
	}
	// An expired lease may already have caused an external effect.
	_, err := s.Pool.Exec(ctx, `UPDATE deliveries SET state='unknown',last_error='Provider lease expired; reconcile before replay' WHERE connection_id<>'' AND state='leased' AND lease_until<now()`)
	return err
}

// Only the first ready delivery in an idle destination lane can be claimed.
// The workspace row lock serializes this decision with mutations and other
// dispatchers. Completed history is excluded by the partial queue indexes.
const eligibleProvider = `d.connection_id<>'' AND d.state='queued' AND d.next_at<=now()
 AND NOT EXISTS(SELECT 1 FROM deliveries busy WHERE busy.connection_id<>'' AND busy.scope=d.scope AND busy.connection_id=d.connection_id AND busy.address=d.address AND busy.state='leased')
 AND NOT EXISTS(SELECT 1 FROM deliveries older WHERE older.connection_id<>'' AND older.scope=d.scope AND older.connection_id=d.connection_id AND older.address=d.address AND older.state='queued' AND older.next_at<=now() AND (older.next_at,older.id)<(d.next_at,d.id))`

func (s *Store) dispatchOne(ctx context.Context, publicURL string) (bool, error) {
	if err := s.prepareDispatch(ctx); err != nil {
		return false, err
	}
	job, worked, err := s.claimProvider(ctx)
	if err != nil || job == nil {
		return worked, err
	}
	return worked, s.finishProvider(ctx, publicURL, job)
}

func (s *Store) claimProvider(ctx context.Context) (*providerJob, bool, error) {
	worked := false
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return nil, worked, e
	}
	defer tx.Rollback(ctx)
	// Match mutation lock order: workspace first, then delivery. Skip busy
	// workspaces so cancellation cannot deadlock and other tenants can progress.
	var scope string
	e = tx.QueryRow(ctx, `SELECT id FROM scopes s WHERE NOT recovery_hold AND NOT suspended AND (access_until IS NULL OR access_until>now()) AND (SELECT d.id FROM deliveries d WHERE d.scope=s.id AND `+eligibleProvider+` ORDER BY d.next_at,d.id LIMIT 1) IS NOT NULL ORDER BY last_dispatch_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&scope)
	if e == pgx.ErrNoRows {
		return nil, worked, nil
	}
	if e != nil {
		return nil, worked, e
	}
	d, e := deliveryScan(tx.QueryRow(ctx, `SELECT `+deliveryFields+` FROM deliveries d WHERE scope=$1 AND `+eligibleProvider+` ORDER BY next_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`, scope))
	if ErrorStatus(e) == 404 {
		return nil, worked, nil
	}
	if e != nil {
		return nil, worked, e
	}
	worked = true
	if d.ExpiresAt.Before(time.Now()) || !s.currentDeliveryPolicy(ctx, tx, scope, d) {
		_, e = tx.Exec(ctx, `UPDATE deliveries SET state='cancelled',last_error='Expired or permission revoked' WHERE id=$1`, d.ID)
		if e == nil {
			e = s.advanceProviderRoute(ctx, tx, scope, d, true)
		}
		if e == nil {
			e = tx.Commit(ctx)
		}
		return nil, worked, e
	}
	var c Connection
	var raw, encrypted []byte
	e = tx.QueryRow(ctx, `SELECT id,name,kind,config,secret,enabled FROM connections WHERE scope=$1 AND id=$2`, scope, d.ConnectionID).Scan(&c.ID, &c.Name, &c.Kind, &raw, &encrypted, &c.Enabled)
	if e != nil {
		return nil, worked, e
	}
	_ = json.Unmarshal(raw, &c.Config)
	if !c.Enabled {
		_, e = tx.Exec(ctx, `UPDATE deliveries SET state='cancelled' WHERE id=$1`, d.ID)
		if e == nil {
			e = s.advanceProviderRoute(ctx, tx, scope, d, true)
		}
		if e == nil {
			e = tx.Commit(ctx)
		}
		return nil, worked, e
	}
	if c.Kind == "telegram" && !s.telegramDestination(ctx, tx, scope, c.ID, d.RecipientID, d.Address) {
		_, e = tx.Exec(ctx, `UPDATE deliveries SET state='cancelled',last_error='Telegram identity link changed' WHERE id=$1`, d.ID)
		if e == nil {
			e = s.advanceProviderRoute(ctx, tx, scope, d, true)
		}
		if e == nil {
			e = tx.Commit(ctx)
		}
		return nil, worked, e
	}
	if c.Kind == "discord" && !s.discordDestination(ctx, tx, scope, c.ID, d.RecipientID, d.Address) {
		_, e = tx.Exec(ctx, `UPDATE deliveries SET state='cancelled',last_error='Discord identity link changed' WHERE id=$1`, d.ID)
		if e == nil {
			e = s.advanceProviderRoute(ctx, tx, scope, d, true)
		}
		if e == nil {
			e = tx.Commit(ctx)
		}
		return nil, worked, e
	}
	secret, e := s.unseal(encrypted)
	if e != nil {
		_, updateErr := tx.Exec(ctx, `UPDATE deliveries SET state='failed',last_error='Provider secret cannot be decrypted; verify encryption key' WHERE id=$1`, d.ID)
		if updateErr != nil {
			return nil, worked, updateErr
		}
		if updateErr = s.advanceProviderRoute(ctx, tx, scope, d, true); updateErr != nil {
			return nil, worked, updateErr
		}
		return nil, worked, tx.Commit(ctx)
	}
	c.Secret = string(secret)
	if e = tx.QueryRow(ctx, `UPDATE deliveries SET state='leased',generation=generation+1,attempts=attempts+1,lease_until=now()+interval '30 seconds' WHERE id=$1 RETURNING generation,attempts`, d.ID).Scan(&d.Generation, &d.Attempts); e != nil {
		return nil, worked, e
	}
	if _, e = tx.Exec(ctx, `UPDATE scopes SET last_dispatch_at=now() WHERE id=$1`, scope); e != nil {
		return nil, worked, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, worked, e
	}
	return &providerJob{scope: scope, connection: c, delivery: d}, worked, nil
}

func (s *Store) finishProvider(ctx context.Context, publicURL string, job *providerJob) error {
	scope, c, d := job.scope, job.connection, job.delivery
	// Preserve private route metadata in storage; never send it to a provider.
	delete(d.Payload, "_policy")
	delete(d.Payload, "_cluster")
	result := s.deliver(ctx, c, d, publicURL)
	next := time.Now()
	if result.Retry && d.Attempts < 6 {
		result.State = "queued"
		delay := time.Duration(1<<min(d.Attempts, 8)) * time.Second
		var jitter [1]byte
		_, _ = rand.Read(jitter[:])
		delay += time.Duration(jitter[0]) * time.Millisecond
		if result.RetryAfter > delay {
			delay = result.RetryAfter
		}
		next = next.Add(delay)
	} else if result.Retry {
		result.State = "failed"
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, `UPDATE deliveries SET state=$3,next_at=$4,last_error=$5 WHERE id=$1 AND generation=$2 AND state='leased'`, d.ID, d.Generation, result.State, next, result.Detail)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return Conflict("Provider delivery lease changed.")
	}
	if _, e = tx.Exec(ctx, `INSERT INTO delivery_attempts(id,scope,delivery_id,generation,outcome,detail) VALUES($1,$2,$3,$4,$5,$6)`, ID("att"), scope, d.ID, d.Generation, result.State, result.Detail); e != nil {
		return e
	}
	if result.Receipt != nil {
		conv, _ := d.Payload["conversation_id"].(string)
		if e = s.recordThread(ctx, tx, scope, c.ID, result.Receipt["channel"], result.Receipt["thread"], conv, d.RecipientID); e != nil {
			return e
		}
	}
	if d.Routing.Managed && (result.State == "failed" || result.State == "provider_accepted") {
		if e = s.advanceProviderRoute(ctx, tx, scope, d, result.State == "failed"); e != nil {
			return e
		}
	} else if !d.Routing.Managed && result.State == "failed" {
		if e = s.queueFallback(ctx, tx, scope, d); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) deliver(ctx context.Context, c Connection, d Delivery, publicURL string) DispatchResult {
	text := ""
	if m, ok := d.Payload["message"].(map[string]any); ok {
		text, _ = m["body"].(string)
	}
	if q, ok := d.Payload["request"].(map[string]any); ok {
		text, _ = q["title"].(string)
		text += "\n" + fmt.Sprint(q["body"])
		text += "\n" + publicURL + "/requests/" + fmt.Sprint(q["id"])
	}
	if c.Kind == "slack" || c.Kind == "telegram" || c.Kind == "discord" || c.Kind == "email" {
		prefix, err := s.providerAttribution(ctx, c.ID, d.Payload)
		if err != nil {
			return DispatchResult{State: "failed", Detail: "Message sender could not be verified.", Retry: true}
		}
		text = prefix + text
	}
	if c.Kind == "email" {
		if conv, ok := d.Payload["conversation_id"].(string); ok && conv != "" {
			text += "\n" + strings.TrimRight(publicURL, "/") + "/conversations/" + conv
		}
		return s.sendMail(ctx, c, d.Address, text)
	}
	if c.Kind == "telegram" {
		return s.deliverTelegram(ctx, c, d, text)
	}
	if c.Kind == "discord" {
		return s.deliverDiscord(ctx, c, d, text)
	}
	destination := c.Config["url"]
	body := JSON(d.Payload)
	if c.Kind == "slack" {
		destination = "https://slack.com/api/chat.postMessage"
		thread := ""
		conv, _ := d.Payload["conversation_id"].(string)
		_ = s.Pool.QueryRow(ctx, `SELECT thread_id FROM provider_threads WHERE connection_id=$1 AND conversation_id=$2 AND channel_id=$3 ORDER BY created_at LIMIT 1`, c.ID, conv, d.Address).Scan(&thread)
		slackBody := map[string]any{"channel": d.Address, "text": text, "unfurl_links": false, "unfurl_media": false}
		if thread != "" {
			slackBody["thread_ts"] = thread
		}
		if q, ok := d.Payload["request"].(map[string]any); ok && q["kind"] == "approval" && d.Payload["type"] == "request" {
			value := string(JSON(map[string]any{"request_id": q["id"], "version": q["version"]}))
			slackBody["blocks"] = []any{map[string]any{"type": "section", "text": map[string]any{"type": "plain_text", "text": text}}, map[string]any{"type": "actions", "elements": []any{map[string]any{"type": "button", "action_id": "beepgrid_approve", "text": map[string]string{"type": "plain_text", "text": "Approve"}, "value": value, "style": "primary"}, map[string]any{"type": "button", "action_id": "beepgrid_deny", "text": map[string]string{"type": "plain_text", "text": "Deny"}, "value": value}}}}
		}
		body = JSON(slackBody)
	}
	if c.Kind != "slack" {
		if _, e := s.validURL(ctx, destination); e != nil {
			return DispatchResult{State: "failed", Detail: "Provider URL is no longer permitted."}
		}
	}
	req, e := http.NewRequestWithContext(ctx, "POST", destination, bytes.NewReader(body))
	if e != nil {
		return DispatchResult{State: "failed", Detail: "Invalid provider request.", Retry: false}
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Kind == "slack" {
		var secrets struct {
			BotToken string `json:"bot_token"`
		}
		if json.Unmarshal([]byte(c.Secret), &secrets) == nil && secrets.BotToken != "" {
			req.Header.Set("Authorization", "Bearer "+secrets.BotToken)
		} else {
			req.Header.Set("Authorization", "Bearer "+c.Secret)
		}
	} else {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		req.Header.Set("X-Beepgrid-Timestamp", ts)
		req.Header.Set("X-Beepgrid-Signature", Signature(c.Secret, ts, body))
		req.Header.Set("X-Beepgrid-Event-ID", d.ID)
	}
	transport := s.providerTransport
	if transport == nil {
		// Only manually assembled test stores use the fallback. OpenExisting owns
		// one bounded transport and closes idle connections with the store.
		t := s.newProviderTransport()
		defer t.CloseIdleConnections()
		transport = t
	}
	client := &http.Client{Timeout: 12 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(req)
	if e != nil {
		return DispatchResult{State: "unknown", Detail: "Provider response was not confirmed.", Retry: false}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 65536))
	if resp.StatusCode == 429 || resp.StatusCode == 503 || resp.StatusCode == 502 || resp.StatusCode == 504 {
		return DispatchResult{State: "failed", Detail: fmt.Sprintf("Temporary provider status %d", resp.StatusCode), Retry: true, RetryAfter: retryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DispatchResult{State: "failed", Detail: fmt.Sprintf("Provider rejected send with status %d", resp.StatusCode), Retry: false}
	}
	if c.Kind == "slack" {
		var result struct {
			OK      bool   `json:"ok"`
			Error   string `json:"error"`
			TS      string `json:"ts"`
			Channel string `json:"channel"`
		}
		if json.Unmarshal(raw, &result) != nil {
			return DispatchResult{State: "unknown", Detail: "Slack response was not readable."}
		}
		if !result.OK {
			return DispatchResult{State: "failed", Detail: "Slack did not accept the message."}
		}
		conv, _ := d.Payload["conversation_id"].(string)
		thread := result.TS
		_ = s.Pool.QueryRow(ctx, `SELECT thread_id FROM provider_threads WHERE connection_id=$1 AND conversation_id=$2 AND channel_id=$3 ORDER BY created_at LIMIT 1`, c.ID, conv, d.Address).Scan(&thread)
		if result.TS != "" {
			return DispatchResult{State: "provider_accepted", Detail: "Slack accepted the message.", Receipt: map[string]string{"channel": d.Address, "thread": thread}}
		}
	}
	return DispatchResult{State: "provider_accepted", Detail: "Provider accepted the message.", Retry: false}
}
func safeError(e error) string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%T", e)
}

func (s *Store) InboundConnection(ctx context.Context, id string) (Connection, string, error) {
	var c Connection
	var raw, encrypted []byte
	var scope string
	e := s.Pool.QueryRow(ctx, `SELECT id,scope,name,kind,config,secret,enabled,version FROM connections WHERE id=$1 AND enabled`, id).Scan(&c.ID, &scope, &c.Name, &c.Kind, &raw, &encrypted, &c.Enabled, &c.Version)
	if e != nil {
		return c, "", Missing()
	}
	_ = json.Unmarshal(raw, &c.Config)
	plain, e := s.unseal(encrypted)
	c.Secret = string(plain)
	return c, scope, e
}
func (s *Store) SlackActor(ctx context.Context, scope, userID, teamID string) (Actor, error) {
	p, e := scanPrincipal(s.Pool.QueryRow(ctx, `SELECT `+principalFields+` FROM principals WHERE scope=$1 AND kind='person' AND metadata->>'slack_user_id'=$2 AND metadata->>'slack_team_id'=$3 AND NOT disabled`, scope, userID, teamID))
	if e != nil {
		return Actor{}, Denied()
	}
	return Actor{Principal: p, Scope: scope, CredentialKind: "provider"}, nil
}
func (s *Store) HasProviderEvent(ctx context.Context, scope, connection, id string) bool {
	var found bool
	_ = s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provider_events WHERE scope=$1 AND connection_id=$2 AND external_id=$3)`, scope, connection, id).Scan(&found)
	return found
}
func (s *Store) RecordProviderEvent(ctx context.Context, scope, connection, id string) error {
	return s.tx(ctx, Actor{Scope: scope}, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO provider_events(scope,connection_id,external_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, scope, connection, id)
		return e
	})
}

func (s *Store) WebhookActor(ctx context.Context, scope, connection, id string) (Actor, error) {
	p, e := principal(ctx, s.Pool, scope, id)
	if e != nil || p.Disabled || p.Kind != "service" {
		return Actor{}, Denied()
	}
	return Actor{Principal: p, Scope: scope, CredentialKind: "provider", ProviderConnection: connection}, nil
}

func retryAfter(raw string, now time.Time) time.Duration {
	var wait time.Duration
	if n, e := strconv.Atoi(raw); e == nil {
		wait = time.Duration(n) * time.Second
	} else if at, e := http.ParseTime(raw); e == nil {
		wait = at.Sub(now)
	}
	if wait < 0 {
		return 0
	}
	if wait > time.Hour {
		return time.Hour
	}
	return wait
}

// Fallback is only scheduled after a definite failure. Unknown outcomes remain for reconciliation.
func (s *Store) queueFallback(ctx context.Context, db DB, scope string, previous Delivery) error {
	if diagnostic, ok := previous.Payload["diagnostic"].(bool); ok && diagnostic {
		return nil
	}

	d, e := deliveryScan(db.QueryRow(ctx, `SELECT `+deliveryFields+` FROM deliveries WHERE scope=$1 AND id=$2`, scope, previous.ID))
	if e != nil {
		return e
	}
	rows, e := db.Query(ctx, `SELECT x.connection_id,x.address FROM destinations x JOIN connections c ON c.id=x.connection_id AND c.scope=x.scope WHERE x.scope=$1 AND x.principal_id=$2 AND x.mode='fallback' AND c.enabled AND NOT EXISTS(SELECT 1 FROM deliveries d WHERE d.scope=x.scope AND d.operation_id=$3 AND d.recipient_id=x.principal_id AND d.connection_id=x.connection_id AND d.address=x.address) ORDER BY x.priority,x.id`, scope, d.RecipientID, d.OperationID)
	if e != nil {
		return e
	}
	type route struct{ id, address string }
	routes := []route{}
	for rows.Next() {
		var r route
		if e = rows.Scan(&r.id, &r.address); e != nil {
			rows.Close()
			return e
		}
		routes = append(routes, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, r := range routes {
		d.ConnectionID = r.id
		d.Address = r.address
		if !s.currentDeliveryPolicy(ctx, db, scope, d) {
			continue
		}
		_, e = db.Exec(ctx, `WITH queued AS (INSERT INTO deliveries(id,scope,operation_id,recipient_id,connection_id,address,payload,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING RETURNING id) SELECT pg_notify('beepgrid_provider','') FROM queued`, ID("del"), scope, d.OperationID, d.RecipientID, r.id, r.address, JSON(d.Payload), d.ExpiresAt)
		return e
	}
	return nil
}

func (s *Store) RemoveDestination(ctx context.Context, a Actor, id string, observed ...int64) error {
	return s.tx(ctx, a, func(tx pgx.Tx) error {
		var connection, pid string
		if e := tx.QueryRow(ctx, `SELECT connection_id,principal_id FROM destinations WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&connection, &pid); e != nil {
			return normalize(e)
		}
		if !s.mayManageDestination(ctx, tx, a, connection, pid) {
			return Denied()
		}
		version := int64(0)
		if len(observed) > 0 {
			version = observed[0]
		}
		if e := checkRevision(ctx, tx, a.Scope, "destinations", id, version); e != nil {
			return e
		}

		tag, e := tx.Exec(ctx, `DELETE FROM destinations WHERE scope=$1 AND id=$2`, a.Scope, id)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return Missing()
		}
		return event(ctx, tx, a, "destination.removed", "", a.ID, map[string]string{"id": id})
	})
}
