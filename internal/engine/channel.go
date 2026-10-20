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
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type Connection struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Kind             string            `json:"kind"`
	Config           map[string]string `json:"config"`
	Enabled          bool              `json:"enabled"`
	SecretConfigured bool              `json:"secret_configured"`
	Secret           string            `json:"secret,omitempty"`
}
type Destination struct {
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
	return g.Seal(nonce, nonce, plain, []byte("notif/connection/v1")), nil
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
	return g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], []byte("notif/connection/v1"))
}
func (s *Store) Connections(ctx context.Context, a Actor) ([]Connection, error) {
	if !a.Admin {
		return nil, Denied()
	}
	rows, e := s.Pool.Query(ctx, `SELECT id,name,kind,config,enabled,octet_length(secret)>0 FROM connections WHERE scope=$1 ORDER BY name`, a.Scope)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Connection{}
	for rows.Next() {
		var c Connection
		var raw []byte
		if e = rows.Scan(&c.ID, &c.Name, &c.Kind, &raw, &c.Enabled, &c.SecretConfigured); e != nil {
			return nil, e
		}
		_ = json.Unmarshal(raw, &c.Config)
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *Store) SaveConnection(ctx context.Context, a Actor, c Connection) (Connection, error) {
	if !ValidText(c.Name, 100) || len(JSON(c.Config)) > 8192 {
		return c, Bad("A name and bounded configuration are required.")
	}
	if c.Kind != "webhook" && c.Kind != "slack" && c.Kind != "email" {
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
	if c.Kind == "email" {
		if _, e := mail.ParseAddress(c.Config["from"]); e != nil {
			return c, Bad("A valid sender address is required.")
		}
		if _, _, e := net.SplitHostPort(c.Config["smtp_addr"]); e != nil {
			return c, Bad("smtp_addr must include host and port.")
		}
	}
	e := s.tx(ctx, a, func(tx pgx.Tx) error {
		if !a.Admin {
			return Denied()
		}
		if c.ID == "" {
			if len(c.Secret) < 16 {
				return Bad("A provider token or signing secret of at least 16 characters is required.")
			}
			c.ID = ID("chn")
		}
		var encrypted []byte
		var e error
		if c.Secret != "" {
			encrypted, e = s.seal([]byte(c.Secret))
			if e != nil {
				return e
			}
		} else {
			if e = tx.QueryRow(ctx, `SELECT secret FROM connections WHERE scope=$1 AND id=$2`, a.Scope, c.ID).Scan(&encrypted); e != nil {
				return e
			}
		}
		tag, e := tx.Exec(ctx, `INSERT INTO connections(id,scope,name,kind,config,secret,enabled) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(id) DO UPDATE SET name=EXCLUDED.name,config=EXCLUDED.config,secret=EXCLUDED.secret,enabled=EXCLUDED.enabled WHERE connections.scope=EXCLUDED.scope AND connections.kind=EXCLUDED.kind`, c.ID, a.Scope, c.Name, c.Kind, JSON(c.Config), encrypted, c.Enabled)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return Denied()
		}
		return event(ctx, tx, a, "connection.saved", "", a.ID, map[string]any{"id": c.ID, "kind": c.Kind})
	})
	c.Secret = ""
	c.SecretConfigured = e == nil
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
		if !a.Admin {
			return Denied()
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
		if d.Mode == "primary" {
			if _, e := tx.Exec(ctx, `UPDATE destinations SET mode='fallback' WHERE scope=$1 AND principal_id=$2 AND mode='primary'`, a.Scope, d.PrincipalID); e != nil {
				return e
			}
		}
		if d.ID == "" {
			d.ID = ID("dst")
		}
		if _, e := tx.Exec(ctx, `INSERT INTO destinations(id,scope,connection_id,principal_id,address,mode,priority) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(id) DO UPDATE SET address=EXCLUDED.address,mode=EXCLUDED.mode,priority=EXCLUDED.priority WHERE destinations.scope=EXCLUDED.scope AND destinations.principal_id=EXCLUDED.principal_id AND destinations.connection_id=EXCLUDED.connection_id`, d.ID, a.Scope, d.ConnectionID, d.PrincipalID, d.Address, d.Mode, d.Priority); e != nil {
			return e
		}
		return event(ctx, tx, a, "destination.created", "", d.PrincipalID, d)
	})
	return d, err
}
func (s *Store) Destinations(ctx context.Context, a Actor) ([]Destination, error) {
	if !a.Admin {
		return nil, Denied()
	}
	rows, e := s.Pool.Query(ctx, `SELECT id,connection_id,principal_id,address,mode,priority FROM destinations WHERE scope=$1`, a.Scope)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Destination{}
	for rows.Next() {
		var d Destination
		if e = rows.Scan(&d.ID, &d.ConnectionID, &d.PrincipalID, &d.Address, &d.Mode, &d.Priority); e != nil {
			return nil, e
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (s *Store) LinkSlackAccount(ctx context.Context, a Actor, personID, userID, teamID string) error {
	return s.tx(ctx, a, func(tx pgx.Tx) error {
		if !a.Admin {
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
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && !(s.AllowLocal && u.Scheme == "http")) {
		return nil, Bad("An HTTPS provider URL is required.")
	}
	ips, e := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if e != nil || len(ips) == 0 {
		return nil, Bad("Provider host could not be resolved.")
	}
	for _, ip := range ips {
		if !s.AllowLocal && (ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() || ip.IP.IsUnspecified() || ip.IP.IsMulticast()) {
			return nil, Bad("Private provider addresses are disabled.")
		}
	}
	return u, nil
}
func (s *Store) dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, e := net.SplitHostPort(address)
	if e != nil {
		return nil, e
	}
	ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
	if e != nil {
		return nil, e
	}
	for _, ip := range ips {
		if !s.AllowLocal && (ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() || ip.IP.IsUnspecified() || ip.IP.IsMulticast()) {
			return nil, fmt.Errorf("private provider address denied")
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no provider address")
	}
	return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
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
}

func (s *Store) DispatchOnce(ctx context.Context, publicURL string) error {
	// Recovering an expired provider lease is ambiguous: the previous send may have succeeded.
	if _, e := s.Pool.Exec(ctx, `UPDATE deliveries SET state='unknown',last_error='Provider lease expired; reconcile before replay' WHERE connection_id<>'' AND state='leased' AND lease_until<now()`); e != nil {
		return e
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	d, e := deliveryScan(tx.QueryRow(ctx, `SELECT `+deliveryFields+` FROM deliveries WHERE connection_id<>'' AND state='queued' AND next_at<=now() AND scope IN(SELECT id FROM scopes WHERE NOT recovery_hold) ORDER BY next_at LIMIT 1 FOR UPDATE SKIP LOCKED`))
	if e == Missing() || ErrorStatus(e) == 404 {
		return nil
	}
	if e != nil {
		return e
	}
	var scope string
	_ = tx.QueryRow(ctx, `SELECT scope FROM deliveries WHERE id=$1`, d.ID).Scan(&scope)
	if d.ExpiresAt.Before(time.Now()) || !s.currentDeliveryPolicy(ctx, tx, scope, d) {
		_, e = tx.Exec(ctx, `UPDATE deliveries SET state='cancelled',last_error='Expired or permission revoked' WHERE id=$1`, d.ID)
		if e == nil {
			e = tx.Commit(ctx)
		}
		return e
	}
	var c Connection
	var raw, encrypted []byte
	e = tx.QueryRow(ctx, `SELECT id,name,kind,config,secret,enabled FROM connections WHERE scope=$1 AND id=$2`, scope, d.ConnectionID).Scan(&c.ID, &c.Name, &c.Kind, &raw, &encrypted, &c.Enabled)
	if e != nil {
		return e
	}
	_ = json.Unmarshal(raw, &c.Config)
	if !c.Enabled {
		_, e = tx.Exec(ctx, `UPDATE deliveries SET state='cancelled' WHERE id=$1`, d.ID)
		if e == nil {
			e = tx.Commit(ctx)
		}
		return e
	}
	secret, e := s.unseal(encrypted)
	if e != nil {
		_, updateErr := tx.Exec(ctx, `UPDATE deliveries SET state='failed',last_error='Provider secret cannot be decrypted; verify encryption key' WHERE id=$1`, d.ID)
		if updateErr != nil {
			return updateErr
		}
		return tx.Commit(ctx)
	}
	c.Secret = string(secret)
	if e = tx.QueryRow(ctx, `UPDATE deliveries SET state='leased',generation=generation+1,attempts=attempts+1,lease_until=now()+interval '30 seconds' WHERE id=$1 RETURNING generation,attempts`, d.ID).Scan(&d.Generation, &d.Attempts); e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	delete(d.Payload, "_policy")
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
	tx, e = s.Pool.Begin(ctx)
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
	if result.State == "failed" {
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
	if c.Kind == "email" {
		if e := s.sendMail(ctx, c, d.Address, text); e != nil {
			return DispatchResult{State: "unknown", Detail: "SMTP outcome uncertain: " + safeError(e), Retry: false}
		}
		return DispatchResult{State: "provider_accepted", Detail: "SMTP server accepted the message.", Retry: false}
	}
	destination := c.Config["url"]
	body := JSON(d.Payload)
	if c.Kind == "slack" {
		destination = "https://slack.com/api/chat.postMessage"
		slackBody := map[string]any{"channel": d.Address, "text": text, "unfurl_links": false, "unfurl_media": false}
		if q, ok := d.Payload["request"].(map[string]any); ok && q["kind"] == "approval" && d.Payload["type"] == "request" {
			value := string(JSON(map[string]any{"request_id": q["id"], "version": q["version"]}))
			slackBody["blocks"] = []any{map[string]any{"type": "section", "text": map[string]any{"type": "plain_text", "text": text}}, map[string]any{"type": "actions", "elements": []any{map[string]any{"type": "button", "action_id": "notif_approve", "text": map[string]string{"type": "plain_text", "text": "Approve"}, "value": value, "style": "primary"}, map[string]any{"type": "button", "action_id": "notif_deny", "text": map[string]string{"type": "plain_text", "text": "Deny"}, "value": value}}}}
		}
		body = JSON(slackBody)
	}
	if _, e := s.validURL(ctx, destination); e != nil {
		return DispatchResult{State: "failed", Detail: "Provider URL is no longer permitted.", Retry: false}
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
		req.Header.Set("X-Notif-Timestamp", ts)
		req.Header.Set("X-Notif-Signature", Signature(c.Secret, ts, body))
		req.Header.Set("X-Notif-Event-ID", d.ID)
	}
	client := &http.Client{Timeout: 12 * time.Second, Transport: &http.Transport{DialContext: s.dial, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(req)
	if e != nil {
		return DispatchResult{State: "unknown", Detail: "Provider response was not confirmed.", Retry: false}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 65536))
	if resp.StatusCode == 429 || resp.StatusCode == 503 || resp.StatusCode == 502 || resp.StatusCode == 504 {
		return DispatchResult{State: "failed", Detail: fmt.Sprintf("Temporary provider status %d", resp.StatusCode), Retry: true}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return DispatchResult{State: "failed", Detail: fmt.Sprintf("Provider rejected send with status %d", resp.StatusCode), Retry: false}
	}
	if c.Kind == "slack" {
		var result struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &result) != nil || !result.OK {
			return DispatchResult{State: "failed", Detail: "Slack did not accept the message.", Retry: false}
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
func (s *Store) sendMail(ctx context.Context, c Connection, to, body string) error {
	addr := c.Config["smtp_addr"]
	host, _, e := net.SplitHostPort(addr)
	if e != nil {
		return e
	}
	conn, e := s.dial(ctx, "tcp", addr)
	if e != nil {
		return e
	}
	_ = conn.SetDeadline(time.Now().Add(12 * time.Second))
	client, e := smtp.NewClient(conn, host)
	if e != nil {
		conn.Close()
		return e
	}
	defer client.Close()
	if yes, _ := client.Extension("STARTTLS"); yes {
		if e = client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); e != nil {
			return e
		}
	} else if !s.AllowLocal {
		return fmt.Errorf("SMTP TLS required")
	}
	if c.Config["username"] != "" {
		if e = client.Auth(smtp.PlainAuth("", c.Config["username"], c.Secret, host)); e != nil {
			return e
		}
	}
	from, e := mail.ParseAddress(c.Config["from"])
	if e != nil {
		return e
	}
	recipient, e := mail.ParseAddress(to)
	if e != nil {
		return e
	}
	if e = client.Mail(from.Address); e != nil {
		return e
	}
	if e = client.Rcpt(recipient.Address); e != nil {
		return e
	}
	w, e := client.Data()
	if e != nil {
		return e
	}
	_, e = fmt.Fprintf(w, "From: %s\r\nTo: %s\r\nSubject: Notif update\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", from.String(), recipient.String(), body)
	if e != nil {
		return e
	}
	if e = w.Close(); e != nil {
		return e
	}
	return client.Quit()
}

func (s *Store) InboundConnection(ctx context.Context, id string) (Connection, string, error) {
	var c Connection
	var raw, encrypted []byte
	var scope string
	e := s.Pool.QueryRow(ctx, `SELECT id,scope,name,kind,config,secret,enabled FROM connections WHERE id=$1 AND enabled`, id).Scan(&c.ID, &scope, &c.Name, &c.Kind, &raw, &encrypted, &c.Enabled)
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
	_, e := s.Pool.Exec(ctx, `INSERT INTO provider_events(scope,connection_id,external_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, scope, connection, id)
	return e
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
		_, e = db.Exec(ctx, `INSERT INTO deliveries(id,scope,operation_id,recipient_id,connection_id,address,payload,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`, ID("del"), scope, d.OperationID, d.RecipientID, r.id, r.address, JSON(d.Payload), d.ExpiresAt)
		return e
	}
	return nil
}

func (s *Store) RemoveDestination(ctx context.Context, a Actor, id string) error {
	return s.tx(ctx, a, func(tx pgx.Tx) error {
		if !a.Admin {
			return Denied()
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
