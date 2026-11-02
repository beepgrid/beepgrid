package engine

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// StartChat explicitly creates shared history. This is separate from private
// broadcast APIs, whose recipients must never see each other's messages.
type StartChatInput struct {
	AttachmentIDs []string  `json:"attachment_ids,omitempty"`
	Targets       []Target  `json:"targets"`
	Body          string    `json:"body"`
	Title         string    `json:"title,omitempty"`
	Approval      bool      `json:"approval,omitempty"`
	Choices       []string  `json:"choices,omitempty"`
	Mentions      []Mention `json:"mentions,omitempty"`
}

func (s *Store) StartChat(ctx context.Context, a Actor, in StartChatInput, key string) (SendResult, error) {
	ctx = withAttachmentBinding(ctx, "start_chat", key)
	var out SendResult
	if (!ValidText(in.Body, 32768) && !(in.Body == "" && len(in.AttachmentIDs) > 0)) || len(in.Title) > 200 || len(in.Targets) < 1 || len(in.Targets) > 100 {
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
			if len(members) > 100 {
				return Bad("A shared chat supports up to 100 members.")
			}
			if e = s.checkChatDisclosure(ctx, tx, a, members); e != nil {
				return e
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
			if _, e = tx.Exec(ctx, `UPDATE conversations SET shared=true,manager_id=$3 WHERE scope=$1 AND id=$2`, a.Scope, conv, a.ID); e != nil {
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
				if e = s.askTx(ctx, tx, a, AskInput{AttachmentIDs: in.AttachmentIDs, ConversationID: conv, Kind: kind, Body: in.Body, Choices: in.Choices, Mentions: in.Mentions}, key+"/decision", &request); e != nil {
					return e
				}
				out = SendResult{ConversationID: conv, Messages: []Message{}}
				return nil
			}
			m, e := s.appendMessageFiles(ctx, tx, a, conv, in.Body, recipients, in.Mentions, in.AttachmentIDs)
			if e != nil {
				return e
			}
			out = SendResult{ConversationID: conv, Messages: []Message{m}}
			return nil
		})
	})
	return out, err
}
