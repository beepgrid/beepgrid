package engine

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

func (s *Store) recordThread(ctx context.Context, db DB, scope, connection, channel, thread, conv, person string) error {
	if channel == "" || thread == "" || conv == "" {
		return nil
	}
	var kind string
	if e := db.QueryRow(ctx, `SELECT kind FROM principals WHERE scope=$1 AND id=$2`, scope, person).Scan(&kind); e != nil {
		return e
	}
	if kind != "person" {
		return nil
	}
	_, e := db.Exec(ctx, `INSERT INTO provider_threads(scope,connection_id,channel_id,thread_id,conversation_id,person_id) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, scope, connection, channel, thread, conv, person)
	return e
}
func (s *Store) SlackMessage(ctx context.Context, a Actor, c Connection, channel, thread, body, eventID string) (SendResult, error) {
	var result SendResult
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		hash := Digest(string(JSON([]string{a.ID, channel, thread, body})))
		key := "message/" + eventID
		var previous string
		var raw []byte
		e := tx.QueryRow(ctx, `SELECT input_hash,response FROM provider_events WHERE scope=$1 AND connection_id=$2 AND external_id=$3`, a.Scope, c.ID, key).Scan(&previous, &raw)
		if e == nil {
			if previous != hash {
				return Conflict("Provider event ID reused with different content.")
			}
			return json.Unmarshal(raw, &result)
		}
		if e != pgx.ErrNoRows {
			return e
		}
		var conv string
		e = tx.QueryRow(ctx, `SELECT conversation_id FROM provider_threads WHERE scope=$1 AND connection_id=$2 AND channel_id=$3 AND thread_id=$4`, a.Scope, c.ID, channel, thread).Scan(&conv)
		if e != nil && e != pgx.ErrNoRows {
			return e
		}
		in := SendInput{ConversationID: conv, Body: body}
		if conv == "" {
			if c.Config["default_agent_id"] == "" {
				return Bad("No default agent is configured for a new Slack conversation.")
			}
			in.Target = Target{"agent", c.Config["default_agent_id"]}
			in.Title = "Slack conversation"
		}
		if e = s.sendTx(ctx, tx, a, in, "slack-event/"+c.ID+"/"+eventID, &result); e != nil {
			return e
		}
		if e = s.recordThread(ctx, tx, a.Scope, c.ID, channel, thread, result.ConversationID, a.ID); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `INSERT INTO provider_events(scope,connection_id,external_id,input_hash,response) VALUES($1,$2,$3,$4,$5)`, a.Scope, c.ID, key, hash, JSON(result))
		return e
	})
	return result, err
}
