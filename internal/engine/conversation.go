package engine

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
	"unicode/utf8"
)

type recipient struct {
	Principal
	ExactInstance string
	PolicyTarget  Target
}

type concurrentMessageKey struct{}

func (s *Store) resolve(ctx context.Context, db DB, a Actor, target Target, action, actionName string, descriptor ...*Action) ([]recipient, error) {
	if len(descriptor) > 0 && descriptor[0] != nil {
		actionName = descriptor[0].Name
	}
	if err := target.Validate(); err != nil {
		return nil, err
	}
	out := []recipient{}
	if target.Kind == "group" {
		allowedGroup := s.allowed(ctx, db, a, action, target, "*", actionName, descriptor...)
		rows, e := db.Query(ctx, `SELECT p.id,p.kind,p.external_id,p.name,p.metadata,p.disabled,p.created_at FROM memberships m JOIN principals p ON p.id=m.principal_id AND p.scope=m.scope WHERE m.scope=$1 AND m.group_id=$2 AND NOT p.disabled ORDER BY p.id LIMIT 1001`, a.Scope, target.ID)
		if e != nil {
			return nil, e
		}
		people := []Principal{}
		for rows.Next() {
			p, e := scanPrincipal(rows)
			if e != nil {
				rows.Close()
				return nil, e
			}
			people = append(people, p)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		if len(people) > 1000 {
			return nil, Bad("Audience exceeds the 1000 recipient limit.")
		}
		for _, p := range people {
			pt := Target{p.Kind, p.ID}
			if (allowedGroup || s.allowed(ctx, db, a, action, pt, "*", actionName, descriptor...)) && !s.blocked(ctx, db, a, action, target) && !s.blocked(ctx, db, a, action, pt) {
				if allowedGroup {
					pt = target
				}
				out = append(out, recipient{p, "", pt})
			}
		}
	} else if target.Kind == "instance" {
		var pid string
		if e := db.QueryRow(ctx, `SELECT agent_id FROM instances WHERE scope=$1 AND id=$2`, a.Scope, target.ID).Scan(&pid); e != nil {
			return nil, Missing()
		}
		p, e := principal(ctx, db, a.Scope, pid)
		if e != nil {
			return nil, e
		}
		if p.Disabled || !s.allowed(ctx, db, a, action, Target{"agent", p.ID}, "*", actionName, descriptor...) {
			return nil, Denied()
		}
		out = append(out, recipient{p, target.ID, Target{"agent", p.ID}})
	} else {
		p, e := scanPrincipal(db.QueryRow(ctx, `SELECT `+principalFields+` FROM principals WHERE scope=$1 AND kind=$2 AND (id=$3 OR external_id=$3) AND NOT disabled`, a.Scope, target.Kind, target.ID))
		if e != nil {
			return nil, Missing()
		}
		if !s.allowed(ctx, db, a, action, Target{p.Kind, p.ID}, "*", actionName, descriptor...) {
			return nil, Denied()
		}
		out = append(out, recipient{p, "", Target{p.Kind, p.ID}})
	}
	if len(out) == 0 {
		return nil, &Error{"NO_ELIGIBLE_RECIPIENT", "No permitted recipient is available.", 403}
	}
	return out, nil
}
func canReadConversation(ctx context.Context, db DB, a Actor, id string) bool {
	if a.Admin {
		var exists bool
		_ = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE scope=$1 AND id=$2)`, a.Scope, id).Scan(&exists)
		return exists
	}
	var ok bool
	_ = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM participants WHERE scope=$1 AND conversation_id=$2 AND principal_id=$3 AND left_at IS NULL)`, a.Scope, id, a.ID).Scan(&ok)
	return ok && a.CredentialKind != "enrollment"
}

// messageTitle is presentation only, never a required part of communication.
func messageTitle(body string) string {
	text := strings.TrimSpace(strings.SplitN(body, "\n", 2)[0])
	for len(text) > 180 {
		_, size := utf8.DecodeLastRuneInString(text)
		text = text[:len(text)-size]
	}
	return text
}

func newConversation(ctx context.Context, db DB, a Actor, title string, ids ...string) (string, error) {
	id := ID("con")
	if title == "" {
		title = "Conversation"
	}
	if _, e := db.Exec(ctx, `INSERT INTO conversations(id,scope,title,created_by) VALUES($1,$2,$3,$4)`, id, a.Scope, title, a.ID); e != nil {
		return "", e
	}
	ids = append(ids, a.ID)
	for _, pid := range ids {
		if _, e := db.Exec(ctx, `INSERT INTO participants(scope,conversation_id,principal_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, a.Scope, id, pid); e != nil {
			return "", e
		}
	}
	return id, nil
}
func (s *Store) Conversations(ctx context.Context, a Actor) ([]Conversation, error) {
	ctx = withPolicySnapshot(ctx)
	rows, e := s.Pool.Query(ctx, conversationListSQL(a.Admin), a.Scope, a.ID)
	if e != nil {
		return nil, e
	}
	type entry struct {
		conv   Conversation
		others []Target
	}
	all := []entry{}
	for rows.Next() {
		var c Conversation
		var raw []byte
		if e = rows.Scan(&c.ID, &c.Title, &c.CreatedBy, &c.CreatedAt, &c.LastActivityAt, &c.LastPreview, &c.Shared, &c.Version, &c.ManagerID, &raw); e != nil {
			rows.Close()
			return nil, e
		}
		var others []Target
		if e = json.Unmarshal(raw, &others); e != nil {
			rows.Close()
			return nil, e
		}
		all = append(all, entry{c, others})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []Conversation{}
	for _, x := range all {
		visible := a.Admin || len(x.others) > 0
		if !a.Admin {
			for _, p := range x.others {
				if !s.allowed(ctx, s.Pool, a, "view", p, "", "") {
					visible = false
					break
				}
			}
		}
		if visible {
			out = append(out, x.conv)
		}
	}
	return out, nil
}

func conversationListSQL(admin bool) string {
	candidates := `SELECT c.id,c.title,c.created_by,c.created_at,c.last_activity_at,c.last_preview,c.preview_sequence,c.shared,c.version,c.manager_id FROM conversations c WHERE c.scope=$1 ORDER BY c.last_activity_at DESC,c.id DESC LIMIT 200`
	if !admin {
		// Start with this person's memberships. The lateral lookup keeps a large
		// workspace history from becoming a repeated full-table hash-join scan.
		candidates = `SELECT c.id,c.title,c.created_by,c.created_at,c.last_activity_at,c.last_preview,c.preview_sequence,c.shared,c.version,c.manager_id FROM participants visible
 CROSS JOIN LATERAL (SELECT c.id,c.title,c.created_by,c.created_at,c.last_activity_at,c.last_preview,c.preview_sequence,c.shared,c.version,c.manager_id FROM conversations c WHERE c.scope=visible.scope AND c.id=visible.conversation_id OFFSET 0) c
 WHERE visible.scope=$1 AND visible.principal_id=$2 AND visible.left_at IS NULL ORDER BY c.last_activity_at DESC,c.id DESC LIMIT 200`
	}
	// Aggregate only each selected conversation's participants. A global join
	// otherwise scans the directory again for each authenticated reader.
	return `WITH candidates AS (` + candidates + `)
 SELECT c.id,c.title,c.created_by,c.created_at,c.last_activity_at,CASE WHEN chat_history_allows($1,c.id,$2,` + boolSQL(admin) + `,c.preview_sequence) THEN c.last_preview ELSE '' END,c.shared,c.version,c.manager_id,COALESCE(others.members,'[]') FROM candidates c
 LEFT JOIN LATERAL (SELECT jsonb_agg(jsonb_build_object('id',p.id,'kind',p.kind)) FILTER(WHERE p.id<>$2) AS members
 FROM participants cp JOIN principals p ON p.scope=cp.scope AND p.id=cp.principal_id
 WHERE cp.scope=$1 AND cp.conversation_id=c.id) others ON true ORDER BY c.last_activity_at DESC,c.id DESC`
}

func (s *Store) Conversation(ctx context.Context, a Actor, id string) (Conversation, error) {
	ctx = withPolicySnapshot(ctx)
	c, e := s.readConversationParticipants(ctx, s.Pool, a, id)
	if e != nil {
		return Conversation{}, e
	}
	rows, e := s.Pool.Query(ctx, `SELECT id,conversation_id,sender_id,COALESCE(instance_id,''),body,created_at,mentions,`+messageReceivedSQL+`,EXISTS(SELECT 1 FROM attachment_links l WHERE l.scope=m.scope AND l.message_id=m.id) FROM (SELECT * FROM messages WHERE scope=$1 AND conversation_id=$2 AND ($4 OR created_sequence >= COALESCE((SELECT history_start FROM participants WHERE scope=$1 AND conversation_id=$2 AND principal_id=$3 AND left_at IS NULL),9223372036854775807)) ORDER BY created_at DESC,id DESC LIMIT 500) m ORDER BY created_at,id`, a.Scope, id, a.ID, a.Admin)
	if e != nil {
		return c, e
	}
	defer rows.Close()
	c.Messages = []Message{}
	hasFiles := false
	for rows.Next() {
		var m Message
		var files bool
		if e = rows.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.InstanceID, &m.Body, &m.CreatedAt, &m.Mentions, &m.ReceivedBy, &files); e != nil {
			return c, e
		}
		hasFiles = hasFiles || files
		c.Messages = append(c.Messages, m)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return c, e
	}
	if !hasFiles {
		return c, nil
	}
	return c, s.loadMessageFiles(ctx, s.Pool, a.Scope, c.Messages)
}

// Load membership, current identities and conversation metadata together.
// Bodies are read only after current View checks pass. Keep even disabled
// historical participants in those checks, matching canRead's authority.
func (s *Store) readConversationParticipants(ctx context.Context, db DB, a Actor, id string) (Conversation, error) {
	var c Conversation
	lock := ""
	if ctx.Value(concurrentMessageKey{}) != nil {
		// Preserve one conversation's message order without serializing other
		// conversations. Acquire this before creating the message timestamp.
		lock = " FOR UPDATE OF c"
	}
	rows, e := db.Query(ctx, `SELECT c.id,c.title,c.created_by,c.created_at,c.last_activity_at,c.last_preview,
 c.shared,c.version,c.manager_id,COALESCE(cp.left_at IS NULL,false),COALESCE(cp.history_start,0),COALESCE(p.id,''),COALESCE(p.kind,''),COALESCE(p.external_id,''),COALESCE(p.name,''),COALESCE(p.metadata,'{}'::jsonb),COALESCE(p.disabled,false),COALESCE(p.created_at,c.created_at)
 FROM conversations c LEFT JOIN participants cp ON cp.scope=c.scope AND cp.conversation_id=c.id
 LEFT JOIN principals p ON p.scope=cp.scope AND p.id=cp.principal_id
 WHERE c.scope=$1 AND c.id=$2 ORDER BY p.id`+lock, a.Scope, id)
	if e != nil {
		return c, e
	}
	c.Participants = []Principal{}
	member := false
	for rows.Next() {
		var p Principal
		var active bool
		var floor int64
		var meta []byte
		if e = rows.Scan(&c.ID, &c.Title, &c.CreatedBy, &c.CreatedAt, &c.LastActivityAt, &c.LastPreview, &c.Shared, &c.Version, &c.ManagerID, &active, &floor, &p.ID, &p.Kind, &p.ExternalID, &p.Name, &meta, &p.Disabled, &p.CreatedAt); e != nil {
			rows.Close()
			return Conversation{}, e
		}
		if p.ID != "" {
			if e = json.Unmarshal(meta, &p.Metadata); e != nil {
				rows.Close()
				return Conversation{}, e
			}
			if p.ID == a.ID && active {
				member = true
				c.HistoryStart = floor
			}
			if active {
				c.Participants = append(c.Participants, p)
			} else {
				c.PastParticipants = append(c.PastParticipants, p)
			}
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return Conversation{}, e
	}
	if c.ID == "" || (!a.Admin && (!member || a.CredentialKind == "enrollment")) {
		return Conversation{}, Missing()
	}
	if !a.Admin {
		others := 0
		for _, p := range append(append([]Principal{}, c.Participants...), c.PastParticipants...) {
			if p.ID == a.ID {
				continue
			}
			others++
			if !s.allowed(ctx, db, a, "view", Target{p.Kind, p.ID}, "", "") {
				return Conversation{}, Missing()
			}
		}
		if others == 0 {
			return Conversation{}, Missing()
		}
	}
	if !a.Admin && c.HistoryStart > 0 {
		var origin int64
		if e = db.QueryRow(ctx, `SELECT preview_sequence FROM conversations WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&origin); e != nil {
			return Conversation{}, e
		}
		if origin < c.HistoryStart {
			c.LastPreview = ""
		}
	}
	return c, nil
}
func (s *Store) conversationRecipients(ctx context.Context, db DB, a Actor, id, action string, descriptor ...*Action) ([]recipient, error) {
	actionName := ""
	if len(descriptor) > 0 && descriptor[0] != nil {
		actionName = descriptor[0].Name
	}
	c, e := s.readConversationParticipants(ctx, db, a, id)
	if e != nil {
		return nil, e
	}
	out := []recipient{}
	for _, p := range c.Participants {
		if p.ID == a.ID || p.Disabled {
			continue
		}
		if !s.allowed(ctx, db, a, action, Target{p.Kind, p.ID}, "*", actionName, descriptor...) {
			return nil, Denied()
		}
		out = append(out, recipient{p, "", Target{p.Kind, p.ID}})
	}
	if len(out) == 0 {
		return nil, Bad("Conversation has no active recipient.")
	}
	return out, nil
}
func insertMessage(ctx context.Context, db DB, a Actor, conv, body string) (Message, error) {
	m := Message{ID: ID("msg"), ConversationID: conv, SenderID: a.ID, InstanceID: a.InstanceID, Body: body, CreatedAt: time.Now()}
	_, e := db.Exec(ctx, `INSERT INTO messages(id,scope,conversation_id,sender_id,instance_id,body,created_at) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7)`, m.ID, a.Scope, conv, a.ID, a.InstanceID, body, m.CreatedAt)
	if e != nil {
		return m, e
	}
	return m, event(ctx, db, a, "message.created", conv, "", m)
}
func (s *Store) enqueue(ctx context.Context, db DB, a Actor, r recipient, op, conv, action string, payload map[string]any, expiry time.Time) error {
	if s.recipientBlocked(ctx, db, a, action, r.ID) {
		return Denied()
	}
	descriptor := payloadAction(payload)
	actionName := ""
	if descriptor != nil {
		actionName = descriptor.Name
	}
	payload["_policy"] = map[string]any{"action_name": actionName, "credential_id": a.CredentialID, "provider_connection": a.ProviderConnection, "sender_id": a.ID, "target": r.PolicyTarget, "action": action}
	payload["conversation_id"] = conv
	externalState := "queued"
	if r.Kind == "person" && payload["type"] == "request" {
		var q Request
		_ = json.Unmarshal(JSON(payload["request"]), &q)
		if q.Kind == "question" && q.SharedKey != "" {
			var leader string
			e := db.QueryRow(ctx, `SELECT old.id FROM requests old JOIN request_audience ra ON ra.scope=old.scope AND ra.request_id=old.id WHERE old.scope=$1 AND old.id<>$2 AND old.kind='question' AND old.state='open' AND old.expires_at>now() AND old.shared_key=$3 AND old.title=$4 AND old.body=$5 AND old.choices=$6::jsonb AND ra.principal_id=$7 ORDER BY old.created_at,old.id LIMIT 1`, a.Scope, q.ID, q.SharedKey, q.Title, q.Body, JSON(q.Choices), r.ID).Scan(&leader)
			if e != nil && e != pgx.ErrNoRows {
				return e
			}
			if leader != "" {
				externalState = "waiting"
				payload["_cluster"] = leader
			}
		}
	}
	queued := 0
	if s.allowed(ctx, db, a, action, r.PolicyTarget, "", actionName, descriptor) {
		if _, e := db.Exec(ctx, `INSERT INTO deliveries(id,scope,operation_id,recipient_id,exact_instance,payload,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, ID("del"), a.Scope, op, r.ID, r.ExactInstance, JSON(payload), expiry); e != nil {
			return e
		}
		queued++
	}
	routes, e := s.resolveProviderRoutes(ctx, db, a, []recipient{r}, conv, action, actionName, descriptor)
	if e != nil {
		return e
	}
	for _, route := range routes[r.ID] {
		if externalState == "waiting" {
			route.State = "waiting"
		}
		if e = insertProviderRoute(ctx, db, a.Scope, op, r.ID, payload, expiry, route); e != nil {
			return e
		}
		queued++
	}
	if _, e = db.Exec(ctx, `SELECT pg_notify('beepgrid_provider','')`); e != nil {
		return e
	}
	if queued == 0 {
		return &Error{"NO_ELIGIBLE_ROUTE", "No permitted destination is configured for this recipient.", 403}
	}
	return notifyWork(ctx, db, a.Scope, r.ID)
}
func (s *Store) Send(ctx context.Context, a Actor, in SendInput, key string) (SendResult, error) {
	var out SendResult
	ctx = withAttachmentBinding(context.WithValue(ctx, concurrentMessageKey{}, true), "send", key)
	err := s.mutation(ctx, a, true, func(tx pgx.Tx) error { return s.sendTx(ctx, tx, a, in, key, &out) })
	return out, err
}
func (s *Store) sendTx(ctx context.Context, tx pgx.Tx, a Actor, in SendInput, key string, out *SendResult) error {
	// Resolution may authorize each member through individual contact rules.
	// Share one bounded snapshot across resolution and queueing, inside the
	// workspace-locked transaction, rather than reloading roles per member.
	ctx = withPolicySnapshot(ctx)
	if (!ValidText(in.Body, 32768) && !(in.Body == "" && len(in.AttachmentIDs) > 0)) || len(in.Title) > 200 {
		return Bad("Message body is required and must be at most 32768 bytes.")
	}

	return idempotent(ctx, tx, a, "send", key, in, out, func() error {
		if in.PreviewToken != "" {
			if in.ConversationID != "" {
				return Bad("Recipient previews apply to new dispatches.")
			}
			if e := s.checkDispatchPreview(ctx, tx, a, DispatchPreviewInput{Targets: []Target{in.Target}, Operation: "message"}, in.PreviewToken); e != nil {
				return e
			}
		}

		var rs []recipient
		var e error
		if in.ConversationID != "" {
			rs, e = s.conversationRecipients(ctx, tx, a, in.ConversationID, "message")
		} else {
			rs, e = s.resolve(ctx, tx, a, in.Target, "message", "")
		}
		if e != nil {
			return e
		}
		if in.Title == "" {
			in.Title = messageTitle(in.Body)
		}
		out.Messages = []Message{}
		if in.ReuseDirect && in.ConversationID == "" && len(rs) == 1 && rs[0].ExactInstance == "" {
			in.ConversationID, e = s.existingDirect(ctx, tx, a, rs[0].Principal)
			if e != nil {
				return e
			}
		}
		if in.ConversationID == "" {
			if len(in.Mentions) > 0 {
				return Bad("Mentions require an existing shared conversation.")
			}
			if len(in.AttachmentIDs) > 0 {
				for _, r := range rs {
					conv, err := newConversation(ctx, tx, a, in.Title, r.ID)
					if err != nil {
						return err
					}
					m, err := s.appendMessageFiles(ctx, tx, a, conv, in.Body, []recipient{r}, in.Mentions, in.AttachmentIDs)
					if err != nil {
						return err
					}
					out.Messages = append(out.Messages, m)
				}
				out.ConversationID = out.Messages[0].ConversationID
				return nil
			}
			_, e := s.nativeFanout(ctx, tx, a, in, rs, out)
			return e
		}
		m, e := s.appendMessageFiles(ctx, tx, a, in.ConversationID, in.Body, rs, in.Mentions, in.AttachmentIDs)
		if e != nil {
			return e
		}
		out.Messages = append(out.Messages, m)
		out.ConversationID = in.ConversationID
		return nil
	})
}
func (s *Store) Events(ctx context.Context, a Actor, after int64) ([]Event, error) {
	events, _, err := s.EventPage(ctx, a, after, false)
	return events, err
}

// EventPage returns the scanned cursor even when current permissions hide a whole page.
func (s *Store) EventPage(ctx context.Context, a Actor, after int64, recent bool) ([]Event, int64, error) {
	return s.eventPage(ctx, a, after, recent, false, "")
}

// EventSummaryPage has identical visibility and cursors, but avoids reading
// message bodies for clients that only use events to invalidate fetched data.
func (s *Store) EventSummaryPage(ctx context.Context, a Actor, after int64) ([]Event, int64, error) {
	return s.eventPage(ctx, a, after, false, true, "")
}

// EventConversationPage carries canonical message bodies only for the open
// conversation. Other events retain summary visibility and scanned cursors.
// Selecting an ID never grants access: the ordinary current-policy filter below
// still applies, including after reconnect or permission revocation.
func (s *Store) EventConversationPage(ctx context.Context, a Actor, after int64, conversation string) ([]Event, int64, error) {
	return s.eventPage(ctx, a, after, false, true, conversation)
}

func (s *Store) eventPage(ctx context.Context, a Actor, after int64, recent, summary bool, conversation string) ([]Event, int64, error) {
	ctx = withPolicySnapshot(ctx)
	order := "ASC"
	if recent {
		order = "DESC"
	}

	rows, e := s.Pool.Query(ctx, `SELECT e.sequence,e.kind,e.actor_id,e.conversation_id,e.recipient_id,CASE WHEN $5 AND NOT (e.kind='message.created' AND e.conversation_id=$6 AND $6<>'') THEN 'null'::jsonb ELSE e.data END,e.created_at FROM events e WHERE e.scope=$1 AND e.sequence>$2 AND (e.conversation_id='' OR chat_history_allows(e.scope,e.conversation_id,$4,$3,CASE WHEN e.kind LIKE 'request.%' THEN COALESCE((SELECT created_sequence FROM requests WHERE scope=e.scope AND id=e.data->>'id'),e.sequence) ELSE e.sequence END)) AND ($3 OR (e.conversation_id<>'' AND e.conversation_id IN(SELECT p.conversation_id FROM participants p WHERE p.scope=$1 AND p.principal_id=$4)) OR e.recipient_id=$4) ORDER BY e.sequence `+order+` LIMIT 200`, a.Scope, after, a.Admin, a.ID, summary, conversation)
	if e != nil {
		return nil, after, e
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var x Event
		if e = rows.Scan(&x.Sequence, &x.Kind, &x.ActorID, &x.ConversationID, &x.RecipientID, jsonColumn{&x.Data}, &x.CreatedAt); e != nil {
			return nil, after, e
		}
		out = append(out, x)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, after, e
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, x := range out {
		if x.ConversationID != "" && !seen[x.ConversationID] {
			ids = append(ids, x.ConversationID)
			seen[x.ConversationID] = true
		}
	}
	readable, e := s.readableEventConversations(ctx, a, ids)
	if e != nil {
		return nil, after, e
	}
	visible := []Event{}
	for _, x := range out {
		if x.Sequence > after {
			after = x.Sequence
		}
		if x.ConversationID == "" || readable[x.ConversationID] {
			visible = append(visible, x)
		}
	}
	return visible, after, nil
}
func (s *Store) Preview(ctx context.Context, a Actor, target Target, action string) (map[string]any, error) {
	rs, e := s.resolve(ctx, s.Pool, a, target, action, "")
	if e != nil {
		return nil, e
	}
	list := []Principal{}
	for _, r := range rs {
		if a.Admin || r.ID == a.ID || s.allowed(ctx, s.Pool, a, "view", Target{r.Kind, r.ID}, "", "") {
			list = append(list, r.Principal)
		}
	}
	return map[string]any{"recipients": list, "count": len(rs), "hidden_recipients": len(rs) - len(list), "mode": "private_per_recipient", "sends": false}, nil
}

// Existing history is reused only for this exact pair and only while the sender
// can still read it. A shared room can never be selected by this path.
func (s *Store) existingDirect(ctx context.Context, db DB, a Actor, peer Principal) (string, error) {
	pair := []string{a.ID, peer.ID}
	if pair[0] > pair[1] {
		pair[0], pair[1] = pair[1], pair[0]
	}
	if _, err := db.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,9127))`, a.Scope+"/direct/"+pair[0]+"/"+pair[1]); err != nil {
		return "", err
	}
	var id string
	err := db.QueryRow(ctx, `SELECT c.id FROM conversations c JOIN participants mine ON mine.scope=c.scope AND mine.conversation_id=c.id AND mine.principal_id=$2 JOIN participants theirs ON theirs.scope=c.scope AND theirs.conversation_id=c.id AND theirs.principal_id=$3 WHERE c.scope=$1 AND NOT c.shared AND mine.left_at IS NULL AND theirs.left_at IS NULL AND (SELECT count(*) FROM participants p WHERE p.scope=c.scope AND p.conversation_id=c.id)=2 ORDER BY c.last_activity_at DESC,c.id DESC LIMIT 1`, a.Scope, a.ID, peer.ID).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !s.canRead(ctx, db, a, id) {
		return "", nil
	}
	return id, nil
}
