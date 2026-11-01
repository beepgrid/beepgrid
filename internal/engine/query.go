package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
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

// Use the existing searchable fields directly. Converting a complete row to
// JSON once for every possible field repeatedly serializes large message,
// delivery and plan payloads, including fields that are not searchable.
func querySearchText(table string) string {
	fields := "t.id"
	switch table {
	case "principals":
		fields += ",t.name,t.external_id,t.metadata->>'department',t.metadata->>'email'"
	case "groups", "roles", "connections":
		fields += ",t.name"
	case "requests":
		fields += ",t.title,t.body"
	case "conversations":
		fields += ",t.title,t.last_preview"
	case "messages":
		fields += ",t.body"
	case "attachments":
		fields += ",t.file_name"
	case "shared_answers":
		fields += ",t.question"
	case "overseer_plans":
		fields += ",t.plan->>'prompt'"
	}
	return "concat_ws(' '," + fields + ")"
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
	hash := digestJSON(stable)
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
	args := []any{a.Scope, last, in.Search, a.ID, a.Admin}
	where := "t.scope=$1" + source.extra + " AND t.id>$2 AND ($4::text IS NOT NULL AND $5::boolean IS NOT NULL)"
	// Messages use a chronological, stable key while retaining a single opaque cursor.
	key := "t.id"
	order := key
	if in.Collection == "messages" || in.Collection == "delivery_attempts" || in.Collection == "overseer_plans" || in.Collection == "conversations" {
		stampColumn := "t.created_at"
		if in.Collection == "conversations" {
			stampColumn = "t.last_activity_at"
		}
		out.Order = "created_at_id_ascending"
		key = "(to_char(t.created_at AT TIME ZONE 'UTC','YYYY-MM-DD\"T\"HH24:MI:SS.US')||'/'||t.id)"
		order = "t.created_at,t.id"
		comparison := ">"
		if in.Collection == "overseer_plans" || in.Collection == "conversations" {
			out.Order = "created_at_id_descending"
			order = "t.created_at DESC,t.id DESC"
			comparison = "<"
		}
		key = strings.ReplaceAll(key, "t.created_at", stampColumn)
		order = strings.ReplaceAll(order, "t.created_at", stampColumn)
		if in.Collection == "conversations" {
			out.Order = "last_activity_at_id_descending"
		}
		// Keep the existing sealed cursor's wire format. Only the SQL seek and
		// ordering change: format the bounded result, not every retained row.
		where = "t.scope=$1 AND $2::text=''"
		if last != "" {
			stamp, id, ok := strings.Cut(last, "/")
			created, err := time.Parse("2006-01-02T15:04:05.000000", stamp)
			if !ok || err != nil || id == "" {
				return out, Bad("Invalid chronological cursor.")
			}
			args[1] = created.UTC()
			args = append(args, id)
			where = "t.scope=$1 AND (" + stampColumn + ",t.id)" + comparison + "($2::timestamptz,$6::text)"
		}
	}
	where += " AND ($4::text IS NOT NULL AND $5::boolean IS NOT NULL)"
	switch in.Collection {
	case "messages":
		// The scoped conversation filter below supplies a single membership window.
	case "requests":
		where += " AND (NOT EXISTS(SELECT 1 FROM conversations c WHERE c.scope=t.scope AND c.id=t.conversation_id AND c.shared) OR chat_history_allows(t.scope,t.conversation_id,$4,$5,t.created_sequence))"
	case "attachments":
		history := strings.NewReplacer("$3", "$4", "$4", "$5").Replace(attachmentHistorySQL)
		where += " AND EXISTS(SELECT 1 FROM attachments a LEFT JOIN attachment_links l ON l.scope=a.scope AND l.attachment_id=a.id WHERE a.scope=t.scope AND a.id=t.id AND " + history + ")"
	case "conversations":
		where += " AND ($5 OR EXISTS(SELECT 1 FROM participants cp WHERE cp.scope=t.scope AND cp.conversation_id=t.id AND cp.principal_id=$4 AND cp.left_at IS NULL))"
	}
	if in.Collection == "overseer_plans" {
		args = append(args, a.ID)
		where += " AND t.person_id=$" + strconv.Itoa(len(args))
	}
	where += " AND ($3='' OR strpos(lower(" + strings.Replace(querySearchText(source.table), "t.last_preview", "CASE WHEN chat_history_allows(t.scope,t.id,$4,$5,t.preview_sequence) THEN t.last_preview ELSE '' END", 1) + "),lower($3))>0)"
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
			if in.Collection == "attachments" {
				args = append(args, value)
				n := strconv.Itoa(len(args))
				where += " AND (t.conversation_id=$" + n + " OR EXISTS(SELECT 1 FROM attachment_links l WHERE l.scope=t.scope AND l.attachment_id=t.id AND l.conversation_id=$" + n + "))"
				continue
			}
			if in.Collection == "messages" || in.Collection == "attachments" || in.Collection == "requests" {
				expression = "t.conversation_id"
			}
		case "state":
			if in.Collection == "requests" {
				expression = "CASE WHEN t.state='open' AND t.expires_at<=now() THEN 'expired' ELSE t.state END"
			} else if in.Collection == "deliveries" || in.Collection == "executions" {
				expression = "t.state"
			}
		case "purpose":
			if in.Collection == "groups" {
				expression = "t.purpose"
			}
		case "member_id":
			if in.Collection == "groups" {
				p, e := principal(ctx, s.Pool, a.Scope, value)
				if e != nil || !s.inspectIdentity(ctx, s.Pool, a, p) {
					return out, Missing()
				}
				args = append(args, value)
				where += " AND EXISTS(SELECT 1 FROM memberships m WHERE m.scope=t.scope AND m.group_id=t.id AND m.principal_id=$" + strconv.Itoa(len(args)) + ")"
				continue
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
		case "presence":
			if in.Collection == "agents" {
				if value != "connected" && value != "stale" && value != "offline" {
					return out, Bad("presence must be connected, stale or offline.")
				}
				// One live replica keeps the stable identity connected. Revoked,
				// expired and disabled credentials never count as live presence.
				expression = `CASE WHEN t.disabled THEN 'offline' WHEN EXISTS(SELECT 1 FROM instances i JOIN credentials c ON c.scope=i.scope AND c.id=i.credential_id WHERE i.scope=t.scope AND i.agent_id=t.id AND NOT c.revoked AND c.expires_at>now() AND i.last_seen>now()-interval '45 seconds') THEN 'connected' WHEN EXISTS(SELECT 1 FROM instances i JOIN credentials c ON c.scope=i.scope AND c.id=i.credential_id WHERE i.scope=t.scope AND i.agent_id=t.id AND NOT c.revoked AND c.expires_at>now() AND i.last_seen>now()-interval '2 minutes') THEN 'stale' ELSE 'offline' END`
			}
		case "project", "type", "parent_id", "enrolled_by", "department", "manager_id":
			if source.table == "principals" {
				expression = "t.metadata->>'" + name + "'"
			}
		case "recipient_id":
			if in.Collection == "requests" {
				args = append(args, value)
				where += " AND EXISTS(SELECT 1 FROM request_audience audience WHERE audience.scope=t.scope AND audience.request_id=t.id AND audience.principal_id=$" + strconv.Itoa(len(args)) + ")"
				continue
			}
		case "participant_id":
			if in.Collection == "conversations" {
				args = append(args, value)
				where += " AND EXISTS(SELECT 1 FROM participants member WHERE member.scope=t.scope AND member.conversation_id=t.id AND member.principal_id=$" + strconv.Itoa(len(args)) + ")"
				continue
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
		c, err := s.readConversationParticipants(ctx, s.Pool, a, id)
		if err != nil {
			return out, err
		}
		if in.Collection == "messages" {
			floor := c.HistoryStart
			if a.Admin {
				floor = 0
			}
			args = append(args, id, floor)
			convParam := "$" + strconv.Itoa(len(args)-1)
			floorParam := "$" + strconv.Itoa(len(args))
			where += " AND t.created_sequence >= " + floorParam + " AND ($5 OR EXISTS(SELECT 1 FROM participants WHERE scope=$1 AND conversation_id=" + convParam + " AND principal_id=$4 AND left_at IS NULL AND history_start=" + floorParam + "))"
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
	ctx = context.WithValue(ctx, attachmentQueryConversationKey{}, in.Filters["conversation_id"])
	// Read candidate IDs only. No private payload or secret enters the response
	// until the per-resource reader has enforced current visibility.
	rows, e := s.Pool.Query(ctx, "SELECT t.id,"+key+" FROM "+source.table+" t WHERE "+where+" ORDER BY "+order+" LIMIT 501", args...)
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
	var bulk map[string]queryItem
	batchEnd := 0
	scanned := 0
	for index, c := range all {
		if scanned == 500 || len(out.Items) == in.Limit {
			break
		}
		// Avoid hydrating all 500 candidates for a small, mostly visible page.
		// Hidden windows still advance the cursor and never exceed its scan cap.
		if index >= batchEnd {
			batchEnd = min(index+max(32, in.Limit), len(all), 500)
			ids := make([]string, 0, batchEnd-index)
			for _, candidate := range all[index:batchEnd] {
				ids = append(ids, candidate.id)
			}
			bulk, e = s.queryRecords(ctx, a, in.Collection, ids)
			if e != nil {
				return out, e
			}
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
		var item any
		var e error
		if bulk != nil {
			v, ok := bulk[c.id]
			if !ok {
				continue
			}
			item, e = v.value, v.err
		} else {
			item, e = s.queryRecord(ctx, a, in.Collection, c.id)
		}
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
		conv, _ := ctx.Value(attachmentQueryConversationKey{}).(string)
		rows, err := db.Query(ctx, attachmentListSQL+` WHERE a.scope=$1 AND a.id=$2 AND `+attachmentHistorySQL+` ORDER BY COALESCE(l.conversation_id,a.conversation_id)`, a.Scope, id, a.ID, a.Admin)
		if err != nil {
			return nil, err
		}
		candidates := []Attachment{}
		for rows.Next() {
			var x Attachment
			if err = scanAttachmentRow(rows, &x); err != nil {
				rows.Close()
				return nil, err
			}
			candidates = append(candidates, x)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, x := range candidates {
			if (conv == "" || conv == x.ConversationID) && s.canRead(ctx, db, a, x.ConversationID) {
				return x, nil
			}
		}
		return nil, Missing()
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
		items, err := s.queryRecords(ctx, a, "conversations", []string{id})
		if err != nil {
			return nil, err
		}
		if x, ok := items[id]; ok {
			return x.value, x.err
		}
		return nil, Missing()
	case "messages":
		var m Message
		e := db.QueryRow(ctx, `SELECT id,conversation_id,sender_id,COALESCE(instance_id,''),body,created_at,mentions,`+messageReceivedSQL+` FROM messages m WHERE scope=$1 AND id=$2 AND chat_history_allows(scope,conversation_id,$3,$4,created_sequence)`, a.Scope, id, a.ID, a.Admin).Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.InstanceID, &m.Body, &m.CreatedAt, &m.Mentions, &m.ReceivedBy)
		if e != nil {
			return nil, normalize(e)
		}
		if !s.canRead(ctx, db, a, m.ConversationID) {
			return nil, Missing()
		}
		ms := []Message{m}
		if e = s.loadMessageFiles(ctx, db, a.Scope, ms); e != nil {
			return nil, e
		}
		return ms[0], nil
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
		e := db.QueryRow(ctx, `SELECT g.id,g.name,g.version,COALESCE((SELECT jsonb_agg(m.principal_id ORDER BY m.principal_id) FROM memberships m WHERE m.scope=g.scope AND m.group_id=g.id),'[]'),g.created_by,g.purpose,g.icon FROM groups g WHERE g.scope=$1 AND g.id=$2`, a.Scope, id).Scan(&x.ID, &x.Name, &x.Version, &raw, &x.CreatedBy, &x.Purpose, &x.Icon)
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
		visible, err := s.visibleGroupMembers(ctx, a, []Group{x})
		if err != nil {
			return nil, err
		}
		if len(visible) == 0 {
			return nil, Missing()
		}
		return visible[0], nil

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
