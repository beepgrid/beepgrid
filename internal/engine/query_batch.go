package engine

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

type queryItem struct {
	value any
	err   error
}

// Hydrate the bounded candidate window in bulk, then apply the same resource
// policy as the individual reader. Cursor progression still uses scanned IDs,
// including hidden rows. No hydrated private data is returned before policy.
func (s *Store) queryRecords(ctx context.Context, a Actor, collection string, ids []string) (map[string]queryItem, error) {
	out := map[string]queryItem{}
	if len(ids) == 0 {
		return out, nil
	}
	put := func(id string, value any, visible bool) {
		if visible {
			out[id] = queryItem{value: value}
		}
	}
	switch collection {
	case "attachments":
		conv, _ := ctx.Value(attachmentQueryConversationKey{}).(string)
		if conv == "" {
			return nil, Bad("Files require a conversation.")
		}
		xs, err := s.Attachments(ctx, a, conv)
		if err != nil {
			return nil, err
		}
		wanted := map[string]bool{}
		for _, id := range ids {
			wanted[id] = true
		}
		for _, x := range xs {
			if wanted[x.ID] {
				put(x.ID, x, true)
			}
		}
	case "deliveries":
		return s.queryDeliveryRecords(ctx, a, ids)
	case "credentials":
		rows, err := s.Pool.Query(ctx, `SELECT id,principal_id,kind,prefix,expires_at,revoked FROM credentials WHERE scope=$1 AND id=ANY($2::text[])`, a.Scope, ids)
		if err != nil {
			return nil, err
		}
		all := []Credential{}
		principals := []string{}
		for rows.Next() {
			var x Credential
			if err = rows.Scan(&x.ID, &x.PrincipalID, &x.Kind, &x.Prefix, &x.ExpiresAt, &x.Revoked); err != nil {
				rows.Close()
				return nil, err
			}
			all = append(all, x)
			principals = append(principals, x.PrincipalID)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		people, err := principalsByIDs(ctx, s.Pool, a.Scope, principals)
		if err != nil {
			return nil, err
		}
		for _, x := range all {
			if p, ok := people[x.PrincipalID]; ok {
				put(x.ID, x, a.ID == p.ID || s.mayIssueCredential(ctx, s.Pool, a, p))
			}
		}
	case "people", "agents", "services":
		people, err := principalsByIDs(ctx, s.Pool, a.Scope, ids)
		if err != nil {
			return nil, err
		}
		for id, p := range people {
			put(id, p, s.inspectIdentity(ctx, s.Pool, a, p))
		}
	case "groups":
		rows, err := s.Pool.Query(ctx, `SELECT g.id,g.name,g.version,COALESCE(jsonb_agg(m.principal_id ORDER BY m.principal_id) FILTER(WHERE m.principal_id IS NOT NULL),'[]'),g.created_by,g.purpose FROM groups g LEFT JOIN memberships m ON m.scope=g.scope AND m.group_id=g.id WHERE g.scope=$1 AND g.id=ANY($2::text[]) GROUP BY g.id`, a.Scope, ids)
		if err != nil {
			return nil, err
		}
		groups := []Group{}
		for rows.Next() {
			var g Group
			if err = rows.Scan(&g.ID, &g.Name, &g.Version, &g.Members, &g.CreatedBy, &g.Purpose); err != nil {
				rows.Close()
				return nil, err
			}
			groups = append(groups, g)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		groups, err = s.visibleGroupMembers(ctx, a, groups)
		if err != nil {
			return nil, err
		}
		for _, g := range groups {
			put(g.ID, g, true)
		}
	case "roles":
		rows, err := s.Pool.Query(ctx, `SELECT id,name,rules,version FROM roles WHERE scope=$1 AND id=ANY($2::text[])`, a.Scope, ids)
		if err != nil {
			return nil, err
		}
		all := []Role{}
		for rows.Next() {
			var r Role
			var raw []byte
			if err = rows.Scan(&r.ID, &r.Name, &raw, &r.Version); err != nil {
				rows.Close()
				return nil, err
			}
			if err = json.Unmarshal(raw, &r.Rules); err != nil {
				rows.Close()
				return nil, err
			}
			all = append(all, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, r := range all {
			put(r.ID, r, s.roleVisible(ctx, s.Pool, a, r.ID))
		}
	case "requests":
		fields := strings.Replace(requestFields, "r.conversation_id", "CASE WHEN $3 OR r.requester_id=$4 THEN r.conversation_id ELSE COALESCE(ra.conversation_id,r.conversation_id) END", 1)
		rows, err := s.Pool.Query(ctx, `SELECT `+fields+`,p.kind FROM requests r JOIN principals p ON p.scope=r.scope AND p.id=r.requester_id LEFT JOIN request_audience ra ON ra.scope=r.scope AND ra.request_id=r.id AND ra.principal_id=$4 WHERE r.scope=$1 AND r.id=ANY($2::text[]) AND ($3 OR r.requester_id=$4 OR ra.principal_id IS NOT NULL OR EXISTS(SELECT 1 FROM conversations cr JOIN participants cp ON cp.scope=cr.scope AND cp.conversation_id=cr.id WHERE cr.scope=r.scope AND cr.id=r.conversation_id AND cr.shared AND cp.principal_id=$4 AND cp.left_at IS NULL))`, a.Scope, ids, a.Admin, a.ID)
		if err != nil {
			return nil, err
		}
		type item struct {
			r    Request
			kind string
		}
		all := []item{}
		for rows.Next() {
			var kind string
			r, e := scanRequest(rows, &kind)
			if e != nil {
				rows.Close()
				return nil, e
			}
			all = append(all, item{r, kind})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		window := []Request{}
		for _, x := range all {
			window = append(window, x.r)
		}
		readable, err := s.readableRequestChats(ctx, a, window)
		if err != nil {
			return nil, err
		}
		visible := []Request{}
		for _, x := range all {
			if (!x.r.shared || readable[x.r.ID]) && (a.Admin || a.ID == x.r.RequesterID || s.allowed(ctx, s.Pool, a, "view", Target{x.kind, x.r.RequesterID}, "", "")) {
				visible = append(visible, x.r)
			}
		}
		if err = s.markAnswerSources(ctx, s.Pool, a.Scope, visible); err != nil {
			return nil, err
		}
		if err = s.loadRequestFiles(ctx, s.Pool, a.Scope, visible); err != nil {
			return nil, err
		}
		for _, r := range visible {
			put(r.ID, r, true)
		}
	case "conversations":
		visible, err := s.readableEventConversations(ctx, a, ids)
		if err != nil {
			return nil, err
		}
		rows, err := s.Pool.Query(ctx, `SELECT c.id,c.title,c.created_by,c.created_at,c.last_activity_at,CASE WHEN chat_history_allows(c.scope,c.id,$3,$4,c.preview_sequence) THEN c.last_preview ELSE '' END,c.shared,c.version,c.manager_id,COALESCE(jsonb_agg(jsonb_build_object('id',p.id,'kind',p.kind,'name',p.name) ORDER BY p.id) FILTER(WHERE p.id IS NOT NULL AND cp.left_at IS NULL),'[]') FROM conversations c LEFT JOIN participants cp ON cp.scope=c.scope AND cp.conversation_id=c.id LEFT JOIN principals p ON p.scope=cp.scope AND p.id=cp.principal_id WHERE c.scope=$1 AND c.id=ANY($2::text[]) GROUP BY c.id`, a.Scope, ids, a.ID, a.Admin)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var c Conversation
			if err = rows.Scan(&c.ID, &c.Title, &c.CreatedBy, &c.CreatedAt, &c.LastActivityAt, &c.LastPreview, &c.Shared, &c.Version, &c.ManagerID, &c.Participants); err != nil {
				return nil, err
			}
			put(c.ID, c, visible[c.ID])
		}
		return out, rows.Err()
	case "messages":
		statement := `SELECT id,conversation_id,sender_id,COALESCE(instance_id,''),body,created_at,mentions FROM messages WHERE scope=$1 AND id=ANY($2::text[]) AND chat_history_allows(scope,conversation_id,$3,$4,created_sequence)`
		rows, err := s.Pool.Query(ctx, statement, a.Scope, ids, a.ID, a.Admin)
		if err != nil {
			return nil, err
		}
		conversations := []string{}
		pending := map[string]string{}
		for rows.Next() {
			var x Message
			if err = rows.Scan(&x.ID, &x.ConversationID, &x.SenderID, &x.InstanceID, &x.Body, &x.CreatedAt, &x.Mentions); err != nil {
				rows.Close()
				return nil, err
			}
			out[x.ID] = queryItem{value: x}
			pending[x.ID] = x.ConversationID
			conversations = append(conversations, x.ConversationID)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		visible, err := s.readableEventConversations(ctx, a, conversations)
		if err != nil {
			return nil, err
		}
		for id, conv := range pending {
			if !visible[conv] {
				delete(out, id)
			}
		}
		ms := []Message{}
		for _, item := range out {
			if m, ok := item.value.(Message); ok {
				ms = append(ms, m)
			}
		}
		if err = s.loadMessageFiles(ctx, s.Pool, a.Scope, ms); err != nil {
			return nil, err
		}
		for _, m := range ms {
			put(m.ID, m, true)
		}
	case "answers":
		rows, err := s.Pool.Query(ctx, `SELECT `+answerFields+` FROM shared_answers WHERE scope=$1 AND id=ANY($2::text[])`, a.Scope, ids)
		if err != nil {
			return nil, err
		}
		all := []SharedAnswer{}
		publishers := []string{}
		for rows.Next() {
			x, e := scanAnswer(rows)
			if e != nil {
				rows.Close()
				return nil, e
			}
			all = append(all, x)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		managers := map[string]bool{}
		for _, x := range all {
			manager := a.Admin || s.manages(ctx, s.Pool, a, "answer", x.ID) || (x.PublisherID == a.ID && s.canPublish(ctx, s.Pool, a))
			managers[x.ID] = manager
			if !manager {
				publishers = append(publishers, x.PublisherID)
			}
		}
		current, err := s.currentPublishers(ctx, s.Pool, a.Scope, publishers)
		if err != nil {
			return nil, err
		}
		for _, x := range all {
			put(x.ID, x, managers[x.ID] || (contains(x.Audience, a.ID) && !x.Invalidated && x.ValidUntil.After(time.Now()) && current[x.PublisherID]))
		}
	case "instances":
		rows, err := s.Pool.Query(ctx, `SELECT i.id,i.agent_id,i.capabilities,i.progress,i.last_seen,CASE WHEN c.revoked OR c.expires_at<=now() OR p.disabled THEN 'offline' WHEN i.last_seen>now()-interval '45 seconds' THEN 'connected' WHEN i.last_seen>now()-interval '2 minutes' THEN 'stale' ELSE 'offline' END FROM instances i JOIN credentials c ON c.scope=i.scope AND c.id=i.credential_id JOIN principals p ON p.scope=i.scope AND p.id=i.agent_id WHERE i.scope=$1 AND i.id=ANY($2::text[])`, a.Scope, ids)
		if err != nil {
			return nil, err
		}
		all := []Instance{}
		for rows.Next() {
			var x Instance
			var caps, progress []byte
			if err = rows.Scan(&x.ID, &x.AgentID, &caps, &progress, &x.LastSeen, &x.Presence); err != nil {
				rows.Close()
				return nil, err
			}
			if err = json.Unmarshal(caps, &x.Capabilities); err != nil {
				rows.Close()
				return nil, err
			}
			if err = json.Unmarshal(progress, &x.Progress); err != nil {
				rows.Close()
				return nil, err
			}
			all = append(all, x)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, x := range all {
			put(x.ID, x, a.ID == x.AgentID || s.allowed(ctx, s.Pool, a, "view", Target{"agent", x.AgentID}, "", ""))
		}
	default:
		return nil, nil
	}
	return out, nil
}
