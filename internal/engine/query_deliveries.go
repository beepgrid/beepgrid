package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

// A diagnostic page is a bounded read, not a dispatch or delivery lease. Its
// batched proofs match mayReadDelivery/currentDeliveryPolicy and are discarded
// after this page. The worker continues to revalidate inside its transaction.
func (s *Store) queryDeliveryRecords(ctx context.Context, a Actor, ids []string) (map[string]queryItem, error) {
	out := map[string]queryItem{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.Pool.Query(ctx, `SELECT `+deliveryFields+`,created_at,next_at,lease_until,exact_instance,owner_instance FROM deliveries WHERE scope=$1 AND id=ANY($2::text[]) AND chat_delivery_allows(scope,COALESCE(payload->>'conversation_id',''),operation_id,$3,$4)`, a.Scope, ids, a.ID, a.Admin)
	if err != nil {
		return nil, err
	}
	all := []DeliveryEvidence{}
	for rows.Next() {
		var x DeliveryEvidence
		if err = rows.Scan(&x.ID, &x.RecipientID, &x.OperationID, &x.Payload, &x.State, &x.Generation, &x.Delivery.Attempts, &x.LastError, &x.ConnectionID, &x.Address, &x.ExpiresAt, jsonColumn{&x.Routing}, &x.CreatedAt, &x.NextAt, &x.LeaseUntil, &x.ExactInstanceID, &x.OwnerInstanceID); err != nil {
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
	operations := []string{}
	conversations := []string{}
	recipients := []string{}
	for _, x := range all {
		operations = append(operations, x.OperationID, strings.SplitN(x.OperationID, "/", 2)[0])
		recipients = append(recipients, x.RecipientID)
		if c, ok := x.Payload["conversation_id"].(string); ok {
			conversations = append(conversations, c)
		}
	}
	owners := map[string]string{}
	requests := map[string]bool{}
	rows, err = s.Pool.Query(ctx, `SELECT r.id,r.requester_id,p.kind,EXISTS(SELECT 1 FROM request_audience ra WHERE ra.scope=r.scope AND ra.request_id=r.id AND ra.principal_id=$3) FROM requests r JOIN principals p ON p.scope=r.scope AND p.id=r.requester_id WHERE r.scope=$1 AND r.id=ANY($2::text[])`, a.Scope, operations, a.ID)
	if err != nil {
		return nil, err
	}
	type requestOwner struct {
		id, owner, kind string
		audience        bool
	}
	rs := []requestOwner{}
	for rows.Next() {
		var r requestOwner
		if err = rows.Scan(&r.id, &r.owner, &r.kind, &r.audience); err != nil {
			rows.Close()
			return nil, err
		}
		rs = append(rs, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, r := range rs {
		owners[r.id] = r.owner
		requests[r.id] = a.Admin || r.owner == a.ID || (r.audience && s.allowed(ctx, s.Pool, a, "view", Target{r.kind, r.owner}, "", ""))
	}
	visible, err := s.readableEventConversations(ctx, a, conversations)
	if err != nil {
		return nil, err
	}
	people, err := principalsByIDs(ctx, s.Pool, a.Scope, recipients)
	if err != nil {
		return nil, err
	}
	// Only a recipient lacking a readable canonical request needs a live route
	// proof. Other diagnostic viewers may inspect state but still get redaction.
	needs := []Delivery{}
	for _, x := range all {
		if !a.Admin && owners[strings.SplitN(x.OperationID, "/", 2)[0]] != a.ID && x.RecipientID == a.ID && !requests[x.OperationID] {
			if c, ok := x.Payload["conversation_id"].(string); !ok || visible[c] {
				needs = append(needs, x.Delivery)
			}
		}
	}
	routes, err := s.deliveryReadRoutes(ctx, a.Scope, needs)
	if err != nil {
		return nil, err
	}
	allowed := []string{}
	for _, x := range all {
		ownRequest := owners[strings.SplitN(x.OperationID, "/", 2)[0]] == a.ID
		may := a.Admin || ownRequest
		if !may && x.RecipientID == a.ID {
			may = requests[x.OperationID] || routes[x.ID]
		}
		if !may && x.RecipientID != a.ID {
			if p, ok := people[x.RecipientID]; ok {
				may = s.allowed(ctx, s.Pool, a, "view", Target{p.Kind, p.ID}, "", "") && s.allowed(ctx, s.Pool, a, "view", Target{"delivery", x.ID}, "", "")
			}
		}
		if !may {
			continue
		}
		x.ContentVisible = a.Admin || ownRequest || (x.RecipientID == a.ID && requests[x.OperationID])
		if !x.ContentVisible {
			if c, ok := x.Payload["conversation_id"].(string); ok {
				x.ContentVisible = visible[c]
			}
		}
		delete(x.Payload, "_policy")
		if !x.ContentVisible {
			x.Payload = nil
		}
		x.Attempts = QueryPage{Items: []any{}, Order: "created_at_id_ascending"}
		out[x.ID] = queryItem{value: x}
		allowed = append(allowed, x.ID)
	}
	if len(allowed) == 0 {
		return out, nil
	}
	// Eleven rows per visible delivery are enough for ten items plus a cursor.
	// The indexed lateral limit avoids materializing unbounded manual retries.
	rows, err = s.Pool.Query(ctx, `SELECT d.id,x.key,x.generation,x.outcome,x.detail,x.created_at FROM unnest($2::text[]) d(id) CROSS JOIN LATERAL (SELECT (to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US')||'/'||id) key,generation,outcome,detail,created_at FROM delivery_attempts WHERE scope=$1 AND delivery_id=d.id ORDER BY created_at,id LIMIT 11) x ORDER BY d.id,x.key`, a.Scope, allowed)
	if err != nil {
		return nil, err
	}
	last := map[string]string{}
	defer rows.Close()
	for rows.Next() {
		var id, key string
		var attempt DeliveryAttempt
		if err = rows.Scan(&id, &key, &attempt.Generation, &attempt.Outcome, &attempt.Detail, &attempt.CreatedAt); err != nil {
			return nil, err
		}
		item := out[id]
		x := item.value.(DeliveryEvidence)
		if len(x.Attempts.Items) < 10 {
			x.Attempts.Items = append(x.Attempts.Items, attempt)
			last[id] = key
		} else if x.Attempts.NextCursor == "" {
			in := QueryInput{Collection: "delivery_attempts", Filters: map[string]string{"delivery_id": id}, Limit: 10}
			raw, e := s.seal(JSON(queryCursor{a.Scope, a.ID, digestJSON(in), last[id], time.Now().Add(24 * time.Hour).Unix()}))
			if e != nil {
				return nil, e
			}
			x.Attempts.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
		}
		item.value = x
		out[id] = item
	}
	return out, rows.Err()
}

func (s *Store) deliveryReadRoutes(ctx context.Context, scope string, deliveries []Delivery) (map[string]bool, error) {
	out := map[string]bool{}
	if len(deliveries) == 0 {
		return out, nil
	}
	type policy struct {
		CredentialID       string `json:"credential_id"`
		ProviderConnection string `json:"provider_connection"`
		SenderID           string `json:"sender_id"`
		Target             Target `json:"target"`
		Action             string `json:"action"`
		ActionName         string `json:"action_name"`
		Result             string `json:"engine_result"`
	}
	policies := map[string]policy{}
	proof := []map[string]any{}
	senders := []string{}
	for _, d := range deliveries {
		var p policy
		if json.Unmarshal(JSON(d.Payload["_policy"]), &p) != nil {
			continue
		}
		policies[d.ID] = p
		senders = append(senders, p.SenderID)
		rid := ""
		check := false
		if r, ok := d.Payload["request"].(map[string]any); ok && d.Payload["type"] == "request" {
			check = true
			rid, _ = r["id"].(string)
		}
		proof = append(proof, map[string]any{"id": d.ID, "sender": p.SenderID, "credential": p.CredentialID, "provider": p.ProviderConnection, "target_kind": p.Target.Kind, "target_id": p.Target.ID, "recipient": d.RecipientID, "request": rid, "check_request": check, "result": p.Result})
	}
	if len(proof) == 0 {
		return out, nil
	}
	rows, err := s.Pool.Query(ctx, `SELECT x.id,COALESCE(p.kind,''),CASE WHEN x.provider<>'' THEN 'provider' ELSE COALESCE(c.kind,'') END,
 COALESCE(c.kind='admin' OR (c.kind='browser' AND EXISTS(SELECT 1 FROM credentials ac WHERE ac.scope=$1 AND ac.principal_id=p.id AND ac.kind='admin' AND NOT ac.revoked AND (ac.expires_at IS NULL OR ac.expires_at>now()))),false),
 CASE WHEN x.result<>'' THEN EXISTS(SELECT 1 FROM requests er JOIN principals ep ON ep.scope=er.scope AND ep.id=er.requester_id WHERE er.scope=$1 AND er.id=x.result AND er.requester_id=x.recipient AND NOT ep.disabled)
 ELSE COALESCE(x.sender<>'' AND NOT p.disabled AND NOT rp.disabled AND
 (CASE WHEN x.provider<>'' THEN pc.enabled ELSE NOT c.revoked AND (c.expires_at IS NULL OR c.expires_at>now()) END) AND
 (x.target_kind<>'group' OR EXISTS(SELECT 1 FROM memberships m WHERE m.scope=$1 AND m.group_id=x.target_id AND m.principal_id=x.recipient)) AND
 (NOT x.check_request OR EXISTS(SELECT 1 FROM requests r WHERE r.scope=$1 AND r.id=x.request AND r.state='open' AND r.expires_at>now())),false) END
 FROM jsonb_to_recordset($2::jsonb) x(id text,sender text,credential text,provider text,target_kind text,target_id text,recipient text,request text,check_request boolean,result text)
 LEFT JOIN principals p ON p.scope=$1 AND p.id=x.sender LEFT JOIN principals rp ON rp.scope=$1 AND rp.id=x.recipient
 LEFT JOIN credentials c ON c.scope=$1 AND c.id=x.credential AND c.principal_id=x.sender LEFT JOIN connections pc ON pc.scope=$1 AND pc.id=x.provider`, scope, JSON(proof))
	if err != nil {
		return nil, err
	}
	actors := map[string]Actor{}
	valid := map[string]bool{}
	for rows.Next() {
		var id string
		var a Actor
		var ok bool
		if err = rows.Scan(&id, &a.Kind, &a.CredentialKind, &a.Admin, &ok); err != nil {
			rows.Close()
			return nil, err
		}
		a.ID = policies[id].SenderID
		a.Scope = scope
		if a.CredentialKind == "provider" {
			a.Admin = false
		}
		actors[id] = a
		valid[id] = ok
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Bulk effective rules for the actual senders, never workspace-wide grants.
	rows, err = s.Pool.Query(ctx, `SELECT effective.principal_id,r.rules FROM (
 SELECT subject_id principal_id,role_id FROM role_bindings WHERE scope=$1 AND subject_kind='principal' AND subject_id=ANY($2::text[])
 UNION ALL SELECT m.principal_id,b.role_id FROM memberships m JOIN role_bindings b ON b.scope=m.scope AND b.subject_kind='group' AND b.subject_id=m.group_id WHERE m.scope=$1 AND m.principal_id=ANY($2::text[])
 ) effective JOIN roles r ON r.scope=$1 AND r.id=effective.role_id`, scope, senders)
	if err != nil {
		return nil, err
	}
	rules := map[string][]Rule{}
	for rows.Next() {
		var id string
		var raw []byte
		var rr []Rule
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal(raw, &rr); err != nil {
			rows.Close()
			return nil, err
		}
		rules[id] = append(rules[id], rr...)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, d := range deliveries {
		p := policies[d.ID]
		a := actors[d.ID]
		if !valid[d.ID] {
			continue
		}
		if p.Result != "" {
			out[d.ID] = true
			continue
		}
		if a.CredentialKind == "enrollment" {
			continue
		}
		if a.Admin {
			out[d.ID] = s.providerChannelAllowed(ctx, s.Pool, a, d.ConnectionID, d.Address)
			continue
		}
		out[d.ID] = s.rulesAllow(ctx, s.Pool, a, rules[a.ID], p.Action, p.Target, d.ConnectionID, p.ActionName, payloadAction(d.Payload)) && !s.recipientBlocked(ctx, s.Pool, a, p.Action, d.RecipientID) && s.providerChannelAllowed(ctx, s.Pool, a, d.ConnectionID, d.Address)
	}
	return out, nil
}
