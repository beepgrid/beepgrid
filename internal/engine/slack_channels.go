package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Channel settings narrow contact permissions. They never grant contact access.
// A channel has one policy across all People who use it as a destination.
type ProviderChannel struct {
	Address    string   `json:"address"`
	Name       string   `json:"name"`
	Restricted bool     `json:"restricted"`
	Allowed    []Target `json:"allowed"`
	Version    int64    `json:"version"`
}
type SlackChannelPage struct {
	Channels   []ProviderChannel `json:"channels"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

var slackChannelID = regexp.MustCompile(`^[CGD][A-Z0-9]{1,99}$`)

func (s *Store) ProviderChannels(ctx context.Context, a Actor, connection string) ([]ProviderChannel, error) {
	if !s.mayManageConnection(ctx, s.Pool, a, connection) {
		return nil, Denied()
	}
	rows, err := s.Pool.Query(ctx, `SELECT address,name,restricted,allowed,version FROM provider_channel_settings WHERE scope=$1 AND connection_id=$2 ORDER BY name,address`, a.Scope, connection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProviderChannel{}
	for rows.Next() {
		var p ProviderChannel
		if err = rows.Scan(&p.Address, &p.Name, &p.Restricted, jsonColumn{&p.Allowed}, &p.Version); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) SaveProviderChannel(ctx context.Context, a Actor, connection string, in ProviderChannel) (ProviderChannel, error) {
	if !slackChannelID.MatchString(in.Address) || !ValidText(in.Name, 100) || len(in.Allowed) > 100 {
		return in, Bad("Choose a Slack channel and up to 100 agents or Agent roles.")
	}
	if !in.Restricted {
		in.Allowed = []Target{}
	}
	if in.Allowed == nil {
		in.Allowed = []Target{}
	}
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		if !s.mayManageConnection(ctx, tx, a, connection) {
			return Denied()
		}
		var kind string
		if err := tx.QueryRow(ctx, `SELECT kind FROM connections WHERE scope=$1 AND id=$2`, a.Scope, connection).Scan(&kind); err != nil {
			return err
		}
		if kind != "slack" {
			return Bad("Channel selection is available for Slack.")
		}
		seen := map[string]bool{}
		for _, t := range in.Allowed {
			if seen[t.Kind+":"+t.ID] {
				return Bad("Choose each agent or role once.")
			}
			seen[t.Kind+":"+t.ID] = true
			var valid bool
			if t.Kind == "agent" {
				_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM principals WHERE scope=$1 AND id=$2 AND kind='agent' AND NOT disabled)`, a.Scope, t.ID).Scan(&valid)
			} else if t.Kind == "group" {
				_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM groups WHERE scope=$1 AND id=$2 AND purpose='agent_role')`, a.Scope, t.ID).Scan(&valid)
			}
			if !valid {
				return Bad("Choose an existing agent or Agent role in this workspace.")
			}
		}
		var version int64
		err := tx.QueryRow(ctx, `SELECT version FROM provider_channel_settings WHERE scope=$1 AND connection_id=$2 AND address=$3`, a.Scope, connection, in.Address).Scan(&version)
		if err != nil && err != pgx.ErrNoRows {
			return err
		}
		if version != in.Version {
			return Conflict("Channel access changed. Reload before saving.")
		}
		if version == 0 {
			var count int
			if err = tx.QueryRow(ctx, `SELECT count(*) FROM provider_channel_settings WHERE scope=$1 AND connection_id=$2`, a.Scope, connection).Scan(&count); err != nil {
				return err
			}
			if count >= 200 {
				return Bad("Configure up to 200 channels per connection.")
			}
		}
		if err = tx.QueryRow(ctx, `INSERT INTO provider_channel_settings(scope,connection_id,address,name,restricted,allowed) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(scope,connection_id,address) DO UPDATE SET name=EXCLUDED.name,restricted=EXCLUDED.restricted,allowed=EXCLUDED.allowed,version=provider_channel_settings.version+1 RETURNING version`, a.Scope, connection, in.Address, in.Name, in.Restricted, JSON(in.Allowed)).Scan(&in.Version); err != nil {
			return err
		}
		return event(ctx, tx, a, "connection.channel_access_changed", "", a.ID, map[string]any{"connection_id": connection, "address": in.Address, "restricted": in.Restricted})
	})
	return in, err
}

// Evaluate at route selection and again before dispatch, including replies and
// fallbacks. People still need their normal conversation/decision permissions.
func (s *Store) providerChannelAllowed(ctx context.Context, db DB, a Actor, connection, address string) bool {
	if a.Kind != "agent" || connection == "" {
		return true
	}
	channels, err := s.channelRestrictions(ctx, db, a.Scope, connection)
	if err != nil {
		return false
	}
	channel, exists := channels[address]
	if !exists || !channel.Restricted {
		return true
	}
	for _, t := range channel.Allowed {
		if t.Kind == "agent" && t.ID == a.ID || t.Kind == "group" && targetGroupMember(ctx, db, a.Scope, t.ID, a.ID) {
			return true
		}
	}
	return false
}

func (s *Store) channelRestrictions(ctx context.Context, db DB, scope, connection string) (map[string]ProviderChannel, error) {
	key := scope + "\x00" + connection
	snapshot, _ := ctx.Value(policySnapshotKey{}).(*policySnapshot)
	if snapshot != nil {
		snapshot.mu.Lock()
		defer snapshot.mu.Unlock()
		if channels, ok := snapshot.channels[key]; ok {
			return channels, nil
		}
	}
	rows, err := db.Query(ctx, `SELECT address,restricted,allowed FROM provider_channel_settings WHERE scope=$1 AND connection_id=$2 AND restricted`, scope, connection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ProviderChannel{}
	for rows.Next() {
		var c ProviderChannel
		if err = rows.Scan(&c.Address, &c.Restricted, jsonColumn{&c.Allowed}); err != nil {
			return nil, err
		}
		out[c.Address] = c
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if snapshot != nil {
		snapshot.channels[key] = out
	}
	return out, nil
}

// Fetch one bounded page only on explicit request, never while delivering a
// message. Bot membership bounds both public and private channel choices.
func (s *Store) SlackChannels(ctx context.Context, a Actor, connection, cursor string) (SlackChannelPage, error) {
	out := SlackChannelPage{Channels: []ProviderChannel{}}
	if len(cursor) > 512 || strings.ContainsAny(cursor, "\r\n") {
		return out, Bad("Invalid Slack channel cursor.")
	}
	if !s.mayManageConnection(ctx, s.Pool, a, connection) {
		return out, Denied()
	}
	c, scope, err := s.InboundConnection(ctx, connection)
	if err != nil || scope != a.Scope || c.Kind != "slack" {
		return out, Missing()
	}
	var secret struct {
		BotToken string `json:"bot_token"`
	}
	if json.Unmarshal([]byte(c.Secret), &secret) != nil || secret.BotToken == "" {
		return out, Bad("Add the Slack bot token before choosing channels.")
	}
	query := url.Values{"types": {"public_channel,private_channel"}, "exclude_archived": {"true"}, "limit": {"100"}, "team_id": {c.Config["team_id"]}}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://slack.com/api/conversations.list?"+query.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+secret.BotToken)
	transport := s.providerTransport
	if transport == nil {
		t := s.newProviderTransport()
		defer t.CloseIdleConnections()
		transport = t
	}
	client := http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return out, Bad("Slack channels could not be loaded. Try again.")
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 {
		return out, Bad("Slack is rate limiting channel lookups. Try again shortly.")
	}
	if resp.StatusCode != 200 {
		return out, Bad("Slack channels could not be loaded. Check the connection.")
	}
	var result struct {
		OK       bool   `json:"ok"`
		Error    string `json:"error"`
		Channels []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Member   bool   `json:"is_member"`
			Archived bool   `json:"is_archived"`
		} `json:"channels"`
		Meta struct {
			Cursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result) != nil {
		return out, Bad("Slack returned an unreadable channel list.")
	}
	if !result.OK {
		if result.Error == "missing_scope" {
			return out, Bad("Update the Slack app manifest and reinstall it to allow channel selection.")
		}
		return out, Bad("Slack channels could not be loaded. Check bot access.")
	}
	if len(result.Channels) > 100 {
		return out, Bad("Slack returned too many channels. Try again.")
	}
	for _, c := range result.Channels {
		if c.Member && !c.Archived && slackChannelID.MatchString(c.ID) && ValidText(c.Name, 100) {
			out.Channels = append(out.Channels, ProviderChannel{Address: c.ID, Name: "#" + c.Name, Allowed: []Target{}})
		}
	}
	if len(result.Meta.Cursor) > 512 || strings.ContainsAny(result.Meta.Cursor, "\r\n") {
		return out, Bad("Slack returned an invalid channel cursor.")
	}
	out.NextCursor = result.Meta.Cursor
	return out, nil
}
