package engine

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// StartChat explicitly creates shared history. This is separate from private
// broadcast APIs, whose recipients must never see each other's messages.
type StartChatInput struct {
	Targets  []Target  `json:"targets"`
	Body     string    `json:"body"`
	Title    string    `json:"title,omitempty"`
	Approval bool      `json:"approval,omitempty"`
	Choices  []string  `json:"choices,omitempty"`
	Mentions []Mention `json:"mentions,omitempty"`
}

func (s *Store) StartChat(ctx context.Context, a Actor, in StartChatInput, key string) (SendResult, error) {
	var out SendResult
	if !ValidText(in.Body, 32768) || len(in.Title) > 200 || len(in.Targets) < 1 || len(in.Targets) > 100 {
		return out, Bad("Choose recipients and write a message.")
	}
	for _, target := range in.Targets {
		if target.Kind != "person" && target.Kind != "agent" && target.Kind != "service" {
			return out, Bad("Shared chats require individual people, agents or services.")
		}
	}
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		return idempotent(ctx, tx, a, "start_chat", key, in, &out, func() error {
			ctx = withPolicySnapshot(ctx)
			rs, e := s.batchRecipients(ctx, tx, a, DispatchPreviewInput{Targets: in.Targets, Operation: "message"})
			if e != nil {
				return e
			}
			members := []Principal{a.Principal}
			ids := []string{}
			for _, r := range rs {
				if r.ID != a.ID {
					members = append(members, r.Principal)
					ids = append(ids, r.ID)
				}
			}
			if len(ids) == 0 {
				return Bad("Choose someone else to start a chat.")
			}
			// Shared membership is disclosure. Every invited identity must already be
			// allowed to view the other members; creating a room grants no authority.
			for _, member := range members {
				viewer := Actor{Principal: member, Scope: a.Scope}
				if member.ID == a.ID {
					viewer = a
				} else {
					if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credentials WHERE scope=$1 AND principal_id=$2 AND kind='admin' AND NOT revoked AND (expires_at IS NULL OR expires_at>now()))`, a.Scope, member.ID).Scan(&viewer.Admin); e != nil {
						return e
					}
				}
				for _, peer := range members {
					if peer.ID != member.ID && !s.allowed(ctx, tx, viewer, "view", Target{peer.Kind, peer.ID}, "", "") {
						return &Error{"CHAT_VISIBILITY_REQUIRED", "Everyone in a shared chat must be allowed to see its other members. Adjust contact permissions or send separate messages.", 403}
					}
				}
			}
			title := in.Title
			if title == "" {
				for i, member := range members[1:] {
					if i > 0 {
						title += ", "
					}
					title += member.Name
				}
				if len(title) > 200 {
					title = "Group chat"
				}
			}
			conv, e := newConversation(ctx, tx, a, title, ids...)
			if e != nil {
				return e
			}
			recipients, e := s.conversationRecipients(ctx, tx, a, conv, "message")
			if e != nil {
				return e
			}
			if in.Approval || len(in.Choices) > 0 {
				kind := "question"
				if in.Approval {
					kind = "approval"
				}
				var request Request
				if e = s.askTx(ctx, tx, a, AskInput{ConversationID: conv, Kind: kind, Body: in.Body, Choices: in.Choices, Mentions: in.Mentions}, key+"/decision", &request); e != nil {
					return e
				}
				out = SendResult{ConversationID: conv, Messages: []Message{}}
				return nil
			}
			m, e := s.appendConversationMessage(ctx, tx, a, conv, in.Body, recipients, in.Mentions)
			if e != nil {
				return e
			}
			out = SendResult{ConversationID: conv, Messages: []Message{m}}
			return nil
		})
	})
	return out, err
}
