package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"time"
)

// Query is additive: old array endpoints retain their response shapes. A page
// may be empty with a next cursor when its bounded scan found no authorized row.
// Continue until next_cursor is empty. Cursors reveal no hidden resource IDs.
type QueryInput struct {
	Collection string            `json:"collection"`
	Search     string            `json:"search,omitempty"`
	Filters    map[string]string `json:"filters,omitempty"`
	Limit      int               `json:"limit,omitempty"`
	Cursor     string            `json:"cursor,omitempty"`
}
type QueryPage struct {
	Items      []any  `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	Order      string `json:"order"`
}
type queryCursor struct {
	Scope, Actor, Query, Last string
	Until                     int64
}
type querySource struct{ table, extra, order string }

var querySources = map[string]querySource{
	"people": {"principals", " AND t.kind='person'", "id"}, "agents": {"principals", " AND t.kind='agent'", "id"}, "services": {"principals", " AND t.kind='service'", "id"},
	"groups": {"groups", "", "id"}, "roles": {"roles", "", "id"}, "connections": {"connections", "", "id"}, "destinations": {"destinations", "", "id"}, "credentials": {"credentials", "", "id"}, "instances": {"instances", "", "id"},
	"requests": {"requests", "", "id"}, "conversations": {"conversations", "", "id"}, "messages": {"messages", "", "created_at,id"}, "answers": {"shared_answers", "", "id"}, "executions": {"execution_intents", "", "id"}, "deliveries": {"deliveries", "", "id"},
	"delivery_attempts": {"delivery_attempts", "", "created_at,id"}, "attachments": {"attachments", "", "id"}, "overseer_plans": {"overseer_plans", "", "id"},
}

func (s *Store) Query(ctx context.Context, a Actor, in QueryInput) (QueryPage, error) {
	out := QueryPage{Items: []any{}, Order: "id_ascending"}
	source, exists := querySources[in.Collection]
	if !exists {
		return out, Bad("Unsupported collection.")
	}
	if in.Limit == 0 {
		in.Limit = 50
	}
	if in.Limit < 1 || in.Limit > 200 || len(in.Search) > 200 || len(in.Cursor) > 4096 {
		return out, Bad("Invalid query bounds.")
	}
	// Binding includes filters and limit. An opaque cursor is never permission.
	stable := in
	stable.Cursor = ""
	hash := Digest(string(JSON(stable)))
	last := ""
	if in.Cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(in.Cursor)
		if e != nil {
			return out, Bad("Invalid cursor.")
		}
		plain, e := s.unseal(raw)
		if e != nil {
			return out, Bad("Invalid cursor.")
		}
		var c queryCursor
		if json.Unmarshal(plain, &c) != nil || c.Scope != a.Scope || c.Actor != a.ID || c.Query != hash || c.Until < time.Now().Unix() {
			return out, Bad("Cursor does not match this query or has expired.")
		}
		last = c.Last
	}
	args := []any{a.Scope, last, in.Search}
	where := "t.scope=$1" + source.extra + " AND t.id>$2"
	// Messages use a chronological, stable key while retaining a single opaque cursor.
	key := "t.id"
	if in.Collection == "messages" || in.Collection == "delivery_attempts" {
		out.Order = "created_at_id_ascending"
		key = "(to_char(t.created_at AT TIME ZONE 'UTC','YYYY-MM-DD\"T\"HH24:MI:SS.US')||'/'||t.id)"
		where = "t.scope=$1 AND " + key + ">$2"
	}
	where += " AND ($3='' OR strpos(lower(concat_ws(' ',to_jsonb(t)->>'id',to_jsonb(t)->>'name',to_jsonb(t)->>'title',to_jsonb(t)->>'body',to_jsonb(t)->>'question',to_jsonb(t)->>'external_id',to_jsonb(t)->'metadata'->>'department',to_jsonb(t)->'metadata'->>'email')),lower($3))>0)"
	for name, value := range in.Filters {
		expression := ""
		switch name {
		case "id":
			expression = "t.id"
		case "delivery_id":
			if in.Collection == "delivery_attempts" {
				expression = "t.delivery_id"
			}
		case "conversation_id":
			if in.Collection == "messages" || in.Collection == "attachments" || in.Collection == "requests" {
				expression = "t.conversation_id"
			}
		case "state":
			if in.Collection == "requests" {
				expression = "CASE WHEN t.state='open' AND t.expires_at<=now() THEN 'expired' ELSE t.state END"
			} else if in.Collection == "deliveries" || in.Collection == "executions" {
				expression = "t.state"
			}
		case "kind":
			if in.Collection == "requests" || in.Collection == "connections" || in.Collection == "credentials" {
				expression = "t.kind"
			}
		case "disabled":
			if source.table == "principals" {
				if value != "true" && value != "false" {
					return out, Bad("disabled must be true or false.")
				}
				expression = "t.disabled::text"
			}
		case "project", "type", "parent_id", "enrolled_by", "department", "manager_id":
			if source.table == "principals" {
				expression = "t.metadata->>'" + name + "'"
			}
		case "requester_id":
			if in.Collection == "requests" {
				expression = "t.requester_id"
			}
		case "agent_id":
			if in.Collection == "instances" {
				expression = "t.agent_id"
			}
		case "connection_id":
			if in.Collection == "destinations" || in.Collection == "deliveries" {
				expression = "t.connection_id"
			}
		case "principal_id":
			if in.Collection == "destinations" || in.Collection == "credentials" {
				expression = "t.principal_id"
			}
		case "operation_id":
			if in.Collection == "deliveries" {
				expression = "t.operation_id"
			}
		case "request_id":
			if in.Collection == "deliveries" {
				expression = "split_part(t.operation_id,'/',1)"
			}
			if in.Collection == "executions" {
				expression = "t.request_id"
			}
		case "group_id":
			if source.table == "principals" {
				if _, e := s.queryRecord(ctx, a, "groups", value); e != nil {
					return out, e
				}
				args = append(args, value)
				where += " AND EXISTS(SELECT 1 FROM memberships m WHERE m.scope=t.scope AND m.principal_id=t.id AND m.group_id=$" + strconv.Itoa(len(args)) + ")"
				continue
			}
		case "role_id":
			if source.table == "principals" {
				if !s.roleVisible(ctx, s.Pool, a, value) {
					return out, Missing()
				}
				args = append(args, value)
				where += " AND EXISTS(SELECT 1 FROM role_bindings b WHERE b.scope=t.scope AND b.role_id=$" + strconv.Itoa(len(args)) + " AND ((b.subject_kind='principal' AND b.subject_id=t.id) OR (b.subject_kind='group' AND EXISTS(SELECT 1 FROM memberships m WHERE m.scope=b.scope AND m.group_id=b.subject_id AND m.principal_id=t.id))))"
				continue
			}
		}
		if expression == "" || len(value) > 200 {
			return out, Bad("Unsupported collection filter.")
		}
		args = append(args, value)
		where += " AND " + expression + "=$" + strconv.Itoa(len(args))
	}
	if in.Collection == "messages" || in.Collection == "attachments" {
		id := in.Filters["conversation_id"]
		if id == "" {
			return out, Bad("Message history requires a conversation_id.")
		}
		if !s.canRead(ctx, s.Pool, a, id) {
			return out, Missing()
		}
	}
	if in.Collection == "delivery_attempts" {
		id := in.Filters["delivery_id"]
		if id == "" {
			return out, Bad("Attempt history requires delivery_id.")
		}
		d, e := deliveryScan(s.Pool.QueryRow(ctx, `SELECT `+deliveryFields+` FROM deliveries WHERE scope=$1 AND id=$2`, a.Scope, id))
		if e != nil {
			return out, e
		}
		if !s.mayReadDelivery(ctx, s.Pool, a, d) {
			return out, Missing()
		}
	}
	if in.Collection == "overseer_plans" {
		if _, e := s.overseerAccess(ctx, s.Pool, a); e != nil {
			return out, e
		}
	}
	ctx = withPolicySnapshot(ctx)
	// Read candidate IDs only. No private payload or secret enters the response
	// until the per-resource reader has enforced current visibility.
	rows, e := s.Pool.Query(ctx, "SELECT t.id,"+key+" FROM "+source.table+" t WHERE "+where+" ORDER BY "+key+" LIMIT 501", args...)
	if e != nil {
		return out, e
	}
	type candidate struct{ id, key string }
	all := []candidate{}
	for rows.Next() {
		var c candidate
		if e = rows.Scan(&c.id, &c.key); e != nil {
			rows.Close()
			return out, e
		}
		all = append(all, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	scanned := 0
	for _, c := range all {
		if scanned == 500 || len(out.Items) == in.Limit {
			break
		}
		scanned++
		last = c.key
		if role := in.Filters["role_id"]; role != "" {
			visible, e := s.visibleRoleMembership(ctx, a, role, c.id)
			if e != nil {
				return out, e
			}
			if !visible {
				continue
			}
		}
		item, e := s.queryRecord(ctx, a, in.Collection, c.id)
		if e != nil {
			if ErrorStatus(e) == 403 || ErrorStatus(e) == 404 {
				continue
			}
			return out, e
		}
		out.Items = append(out.Items, item)
	}
	if scanned < len(all) {
		raw, e := s.seal(JSON(queryCursor{a.Scope, a.ID, hash, last, time.Now().Add(24 * time.Hour).Unix()}))
		if e != nil {
			return out, e
		}
		out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, nil
}
func (s *Store) queryRecord(ctx context.Context, a Actor, collection, id string) (any, error) {
	db := s.Pool
	switch collection {
	case "attachments":
		var x Attachment
		e := db.QueryRow(ctx, `SELECT id,conversation_id,file_name,media_type,size,sha256,created_at FROM attachments WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&x.ID, &x.ConversationID, &x.FileName, &x.MediaType, &x.Size, &x.SHA256, &x.CreatedAt)
		if e != nil {
			return nil, normalize(e)
		}
		if !s.canRead(ctx, db, a, x.ConversationID) {
			return nil, Missing()
		}
		return x, nil
	case "overseer_plans":
		return s.OverseerPlan(ctx, a, id)
	case "delivery_attempts":
		var x DeliveryAttempt
		var delivery string
		e := db.QueryRow(ctx, `SELECT delivery_id,generation,outcome,detail,created_at FROM delivery_attempts WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&delivery, &x.Generation, &x.Outcome, &x.Detail, &x.CreatedAt)
		if e != nil {
			return nil, normalize(e)
		}
		d, e := deliveryScan(db.QueryRow(ctx, `SELECT `+deliveryFields+` FROM deliveries WHERE scope=$1 AND id=$2`, a.Scope, delivery))
		if e != nil {
			return nil, e
		}
		if !s.mayReadDelivery(ctx, db, a, d) {
			return nil, Missing()
		}
		return x, nil
	case "people", "agents", "services":
		p, e := principal(ctx, db, a.Scope, id)
		if e != nil {
			return nil, e
		}
		if !s.inspectIdentity(ctx, db, a, p) {
			return nil, Missing()
		}
		return p, nil
	case "requests":
		return s.request(ctx, db, a, id)
	case "conversations":
		if !s.canRead(ctx, db, a, id) {
			return nil, Missing()
		}
		var c Conversation
		e := db.QueryRow(ctx, `SELECT id,title,created_by,created_at FROM conversations WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&c.ID, &c.Title, &c.CreatedBy, &c.CreatedAt)
		if e != nil {
			return c, normalize(e)
		}
		// A directory row needs participant presentation, not 500 message bodies
		// or private profile metadata. Visibility is the same canRead check above.
		rows, e := db.Query(ctx, `SELECT p.id,p.kind,p.name FROM participants cp JOIN principals p ON p.scope=cp.scope AND p.id=cp.principal_id WHERE cp.scope=$1 AND cp.conversation_id=$2 ORDER BY p.id`, a.Scope, id)
		if e != nil {
			return c, e
		}
		defer rows.Close()
		c.Participants = []Principal{}
		for rows.Next() {
			var p Principal
			if e = rows.Scan(&p.ID, &p.Kind, &p.Name); e != nil {
				return c, e
			}
			c.Participants = append(c.Participants, p)
		}
		return c, rows.Err()
	case "messages":
		var m Message
		e := db.QueryRow(ctx, `SELECT id,conversation_id,sender_id,COALESCE(instance_id,''),body,created_at FROM messages WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.InstanceID, &m.Body, &m.CreatedAt)
		if e != nil {
			return nil, normalize(e)
		}
		if !s.canRead(ctx, db, a, m.ConversationID) {
			return nil, Missing()
		}
		return m, nil
	case "answers":
		x, e := scanAnswer(db.QueryRow(ctx, `SELECT `+answerFields+` FROM shared_answers WHERE scope=$1 AND id=$2`, a.Scope, id))
		if e != nil {
			return nil, e
		}
		manager := a.Admin || s.manages(ctx, db, a, "answer", id) || (x.PublisherID == a.ID && s.canPublish(ctx, db, a))
		if !manager && (!contains(x.Audience, a.ID) || x.Invalidated || !x.ValidUntil.After(time.Now()) || !s.publisherCurrent(ctx, db, a.Scope, x)) {
			return nil, Missing()
		}
		return x, nil
	case "roles":
		if !s.roleVisible(ctx, db, a, id) {
			return nil, Missing()
		}
		var x Role
		var raw []byte
		e := db.QueryRow(ctx, `SELECT id,name,rules,version FROM roles WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&x.ID, &x.Name, &raw, &x.Version)
		if e == nil {
			e = json.Unmarshal(raw, &x.Rules)
		}
		return x, normalize(e)
	case "groups":
		var x Group
		var raw []byte
		e := db.QueryRow(ctx, `SELECT g.id,g.name,g.version,COALESCE((SELECT jsonb_agg(m.principal_id ORDER BY m.principal_id) FROM memberships m WHERE m.scope=g.scope AND m.group_id=g.id),'[]'),g.created_by FROM groups g WHERE g.scope=$1 AND g.id=$2`, a.Scope, id).Scan(&x.ID, &x.Name, &x.Version, &raw, &x.CreatedBy)
		if e != nil {
			return nil, normalize(e)
		}
		if e = json.Unmarshal(raw, &x.Members); e != nil {
			return nil, e
		}
		x.Member = contains(x.Members, a.ID)
		if !x.Member && !s.manages(ctx, db, a, "group", id) && !s.allowed(ctx, db, a, "view", Target{"group", id}, "", "") {
			return nil, Missing()
		}
		members := []string{}
		for _, pid := range x.Members {
			p, e := principal(ctx, db, a.Scope, pid)
			if e != nil {
				return nil, e
			}
			if s.inspectIdentity(ctx, db, a, p) {
				members = append(members, pid)
			}
		}
		x.MembersComplete = len(members) == len(x.Members)
		x.Members = members
		return x, nil
	case "connections":
		var x Connection
		var raw []byte
		e := db.QueryRow(ctx, `SELECT id,name,kind,config,enabled,octet_length(secret)>28,version FROM connections WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&x.ID, &x.Name, &x.Kind, &raw, &x.Enabled, &x.SecretConfigured, &x.Version)
		if e != nil {
			return nil, normalize(e)
		}
		managed := s.mayManageConnection(ctx, db, a, id)
		var own bool
		if e = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM destinations WHERE scope=$1 AND connection_id=$2 AND principal_id=$3)`, a.Scope, id, a.ID).Scan(&own); e != nil {
			return nil, e
		}
		if !managed && !own && !s.allowed(ctx, db, a, "view", Target{"connection", id}, "", "") {
			return nil, Missing()
		}
		x.Config = map[string]string{}
		if managed {
			e = json.Unmarshal(raw, &x.Config)
		}
		return x, e
	case "destinations":
		var x Destination
		e := db.QueryRow(ctx, `SELECT id,connection_id,principal_id,address,mode,priority,version FROM destinations WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&x.ID, &x.ConnectionID, &x.PrincipalID, &x.Address, &x.Mode, &x.Priority, &x.Version)
		if e != nil {
			return nil, normalize(e)
		}
		if a.ID != x.PrincipalID && !s.mayManageDestination(ctx, db, a, x.ConnectionID, x.PrincipalID) {
			return nil, Missing()
		}
		return x, nil
	case "credentials":
		var x Credential
		e := db.QueryRow(ctx, `SELECT id,principal_id,kind,prefix,expires_at,revoked FROM credentials WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&x.ID, &x.PrincipalID, &x.Kind, &x.Prefix, &x.ExpiresAt, &x.Revoked)
		if e != nil {
			return nil, normalize(e)
		}
		p, e := principal(ctx, db, a.Scope, x.PrincipalID)
		if e != nil {
			return nil, e
		}
		if a.ID != p.ID && !s.mayIssueCredential(ctx, db, a, p) {
			return nil, Missing()
		}
		return x, nil
	case "instances":
		var x Instance
		var caps, progress []byte
		e := db.QueryRow(ctx, `SELECT i.id,i.agent_id,i.capabilities,i.progress,i.last_seen,CASE WHEN c.revoked OR c.expires_at<=now() OR p.disabled THEN 'offline' WHEN i.last_seen>now()-interval '45 seconds' THEN 'connected' WHEN i.last_seen>now()-interval '2 minutes' THEN 'stale' ELSE 'offline' END FROM instances i JOIN credentials c ON c.scope=i.scope AND c.id=i.credential_id JOIN principals p ON p.scope=i.scope AND p.id=i.agent_id WHERE i.scope=$1 AND i.id=$2`, a.Scope, id).Scan(&x.ID, &x.AgentID, &caps, &progress, &x.LastSeen, &x.Presence)
		if e != nil {
			return nil, normalize(e)
		}
		if a.ID != x.AgentID && !s.allowed(ctx, db, a, "view", Target{"agent", x.AgentID}, "", "") {
			return nil, Missing()
		}
		if e = json.Unmarshal(caps, &x.Capabilities); e != nil {
			return nil, e
		}
		e = json.Unmarshal(progress, &x.Progress)
		return x, e
	case "executions":
		var x Execution
		var raw, result []byte
		e := db.QueryRow(ctx, `SELECT id,request_id,executor_id,operation_key,descriptor,state,result FROM execution_intents WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&x.ID, &x.RequestID, &x.ExecutorID, &x.OperationKey, &raw, &x.State, &result)
		if e != nil {
			return nil, normalize(e)
		}
		if !a.Admin && a.ID != x.ExecutorID {
			if _, e = s.request(ctx, db, a, x.RequestID); e != nil {
				return nil, e
			}
		}
		if e = json.Unmarshal(raw, &x.Action); e != nil {
			return nil, e
		}
		if len(result) > 0 {
			e = json.Unmarshal(result, &x.Result)
		}
		return x, e
	case "deliveries":
		d, e := deliveryScan(db.QueryRow(ctx, `SELECT `+deliveryFields+` FROM deliveries WHERE scope=$1 AND id=$2`, a.Scope, id))
		if e != nil {
			return nil, e
		}
		if !s.mayReadDelivery(ctx, db, a, d) {
			return nil, Missing()
		}
		delete(d.Payload, "_policy")
		return s.deliveryEvidence(ctx, a, d)
	}
	return nil, Bad("Unsupported collection.")
}

func (s *Store) visibleRoleMembership(ctx context.Context, a Actor, role, pid string) (bool, error) {
	rows, e := s.Pool.Query(ctx, `SELECT role_id,subject_kind,subject_id FROM role_bindings b WHERE scope=$1 AND role_id=$2 AND ((subject_kind='principal' AND subject_id=$3) OR (subject_kind='group' AND EXISTS(SELECT 1 FROM memberships m WHERE m.scope=b.scope AND m.group_id=b.subject_id AND m.principal_id=$3)))`, a.Scope, role, pid)
	if e != nil {
		return false, e
	}
	bindings := []Binding{}
	for rows.Next() {
		var b Binding
		if e = rows.Scan(&b.RoleID, &b.SubjectKind, &b.SubjectID); e != nil {
			rows.Close()
			return false, e
		}
		bindings = append(bindings, b)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return false, e
	}
	for _, b := range bindings {
		if s.bindingVisible(ctx, s.Pool, a, b) {
			return true, nil
		}
	}
	return false, e
}
