package engine

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

func boolSQL(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// Chat membership is disclosure only. It never grants contact or decision rights.
type ChatMembershipInput struct {
	Operation    string   `json:"operation"`
	Version      int      `json:"version"`
	Title        string   `json:"title,omitempty"`
	Members      []string `json:"members,omitempty"`
	ShareHistory bool     `json:"share_history,omitempty"`
	NextManager  string   `json:"next_manager,omitempty"`
}
type ChatMembershipResult struct {
	ConversationID string `json:"conversation_id"`
	Version        int    `json:"version"`
	Left           bool   `json:"left,omitempty"`
}

func (s *Store) ChangeChatMembership(ctx context.Context, a Actor, id string, in ChatMembershipInput, key string) (ChatMembershipResult, error) {
	var out ChatMembershipResult
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		return idempotent(ctx, tx, a, "chat_membership/"+id, key, in, &out, func() error {
			ctx = withPolicySnapshot(ctx)
			c, err := s.readConversationParticipants(ctx, tx, a, id)
			if err != nil {
				return err
			}
			if !c.Shared {
				return Bad("Member controls are available for shared chats.")
			}
			if c.Version != in.Version {
				return Conflict("This chat changed. Refresh and try again.")
			}
			if in.Operation != "leave" && !a.Admin && c.ManagerID != a.ID {
				return Denied()
			}
			members := map[string]Principal{}
			for _, p := range c.Participants {
				members[p.ID] = p
			}
			switch in.Operation {
			case "rename":
				title := strings.TrimSpace(in.Title)
				if !ValidText(title, 200) {
					return Bad("Enter a chat name, up to 200 characters.")
				}
				_, err = tx.Exec(ctx, `UPDATE conversations SET title=$3 WHERE scope=$1 AND id=$2`, a.Scope, id, title)
			case "add":
				if len(in.Members) < 1 || len(in.Members) > 100 {
					return Bad("Choose up to 100 members.")
				}
				proposed := append(append([]Principal{}, c.Participants...), c.PastParticipants...)
				additions := []Principal{}
				seen := map[string]bool{}
				for _, pid := range in.Members {
					if seen[pid] {
						return Bad("Choose each member once.")
					}
					seen[pid] = true
					if _, ok := members[pid]; ok {
						return Bad("That member is already in this chat.")
					}
					p, e := principal(ctx, tx, a.Scope, pid)
					if e != nil || p.Disabled {
						return Missing()
					}
					if !s.allowed(ctx, tx, a, "message", Target{p.Kind, p.ID}, "*", "") {
						return Denied()
					}
					additions = append(additions, p)
					already := false
					for _, old := range proposed {
						if old.ID == p.ID {
							already = true
						}
					}
					if !already {
						proposed = append(proposed, p)
					}
				}
				if len(c.Participants)+len(additions) > 100 {
					return Bad("A shared chat supports up to 100 members.")
				}
				if err = s.checkChatDisclosure(ctx, tx, a, proposed); err != nil {
					return err
				}
				var floor int64
				if err = tx.QueryRow(ctx, `SELECT sequence+1 FROM scopes WHERE id=$1`, a.Scope).Scan(&floor); err != nil {
					return err
				}
				if in.ShareHistory {
					floor = 0
				}
				for _, p := range additions {
					_, err = tx.Exec(ctx, `INSERT INTO participants(scope,conversation_id,principal_id,history_start) VALUES($1,$2,$3,$4) ON CONFLICT(scope,conversation_id,principal_id) DO UPDATE SET left_at=NULL,history_start=EXCLUDED.history_start`, a.Scope, id, p.ID, floor)
					if err != nil {
						return err
					}
				}
			case "remove", "leave":
				ids := in.Members
				if in.Operation == "leave" {
					if _, ok := members[a.ID]; !ok {
						return Missing()
					}
					ids = []string{a.ID}
				}
				if len(ids) < 1 || len(ids) > 100 {
					return Bad("Choose members to remove.")
				}
				removed := map[string]bool{}
				for _, pid := range ids {
					if _, ok := members[pid]; !ok {
						return Missing()
					}
					removed[pid] = true
				}
				if removed[c.ManagerID] && len(members) > len(removed) {
					if _, ok := members[in.NextManager]; !ok || removed[in.NextManager] {
						return Bad("Choose another member to manage the chat before leaving.")
					}
					if _, err = tx.Exec(ctx, `UPDATE conversations SET manager_id=$3 WHERE scope=$1 AND id=$2`, a.Scope, id, in.NextManager); err != nil {
						return err
					}
				}
				_, err = tx.Exec(ctx, `UPDATE participants SET left_at=now() WHERE scope=$1 AND conversation_id=$2 AND principal_id=ANY($3::text[]) AND left_at IS NULL`, a.Scope, id, ids)
				if err != nil {
					return err
				}
				for _, pid := range ids {
					if err = event(ctx, tx, a, "conversation.access_changed", "", pid, map[string]any{"conversation_id": id, "member": false}); err != nil {
						return err
					}
				}
				out.Left = removed[a.ID]
			default:
				return Bad("Choose rename, add, remove or leave.")
			}
			if err != nil {
				return err
			}
			if err = tx.QueryRow(ctx, `UPDATE conversations SET version=version+1 WHERE scope=$1 AND id=$2 RETURNING version`, a.Scope, id).Scan(&out.Version); err != nil {
				return err
			}
			out.ConversationID = id
			return event(ctx, tx, a, "conversation.membership_changed", id, "", map[string]any{"operation": in.Operation, "members": in.Members, "title": in.Title, "share_history": in.ShareHistory, "version": out.Version})
		})
	})
	return out, err
}
func (s *Store) checkChatDisclosure(ctx context.Context, db DB, a Actor, members []Principal) error {
	for _, member := range members {
		viewer := Actor{Principal: member, Scope: a.Scope}
		if member.ID == a.ID {
			viewer = a
		} else if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credentials WHERE scope=$1 AND principal_id=$2 AND kind='admin' AND NOT revoked AND (expires_at IS NULL OR expires_at>now()))`, a.Scope, member.ID).Scan(&viewer.Admin); err != nil {
			return err
		}
		for _, peer := range members {
			if peer.ID != member.ID && !s.allowed(ctx, db, viewer, "view", Target{peer.Kind, peer.ID}, "", "") {
				return &Error{"CHAT_VISIBILITY_REQUIRED", "Everyone in a shared chat must be allowed to see its other members. Adjust contact permissions or send separate messages.", 403}
			}
		}
	}
	return nil
}

// Request projections may be private. Only explicit shared rooms add readers;
// reading older history still cannot add someone to the original decision audience.
func (s *Store) requestHistoryAllowed(ctx context.Context, db DB, a Actor, r Request) bool {
	if !r.shared {
		return true
	}
	var shared, visible bool
	err := db.QueryRow(ctx, `SELECT c.shared,chat_history_allows(c.scope,c.id,$3,$4,r.created_sequence) FROM requests r JOIN conversations c ON c.scope=r.scope AND c.id=$2 WHERE r.scope=$1 AND r.id=$5`, a.Scope, r.ConversationID, a.ID, a.Admin, r.ID).Scan(&shared, &visible)
	return err == nil && (!shared || (visible && s.canRead(ctx, db, a, r.ConversationID)))
}
func (s *Store) deliveryChatVisible(ctx context.Context, db DB, scope string, d Delivery, person string, admin bool) bool {
	conv, _ := d.Payload["conversation_id"].(string)
	if conv == "" {
		return true
	}
	var visible bool
	err := db.QueryRow(ctx, `SELECT chat_delivery_allows($1,$2,$3,$4,$5)`, scope, conv, d.OperationID, person, admin).Scan(&visible)
	return err == nil && visible
}

// List reads share one membership and policy proof per page, not per card.
func (s *Store) readableRequestChats(ctx context.Context, a Actor, requests []Request) (map[string]bool, error) {
	out := map[string]bool{}
	ids := []string{}
	seen := map[string]bool{}
	for _, r := range requests {
		if r.shared && !seen[r.ConversationID] {
			ids = append(ids, r.ConversationID)
			seen[r.ConversationID] = true
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	visible, err := s.readableEventConversations(ctx, a, ids)
	if err != nil {
		return nil, err
	}
	floors := map[string]int64{}
	if !a.Admin {
		rows, err := s.Pool.Query(ctx, `SELECT conversation_id,history_start FROM participants WHERE scope=$1 AND principal_id=$2 AND conversation_id=ANY($3::text[]) AND left_at IS NULL`, a.Scope, a.ID, ids)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			var floor int64
			if err = rows.Scan(&id, &floor); err != nil {
				rows.Close()
				return nil, err
			}
			floors[id] = floor
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	for _, r := range requests {
		floor, present := floors[r.ConversationID]
		out[r.ID] = visible[r.ConversationID] && (a.Admin || (present && r.createdSequence >= floor))
	}
	return out, nil
}
