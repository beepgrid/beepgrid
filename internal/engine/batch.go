package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"sort"
	"time"
)

type DispatchPreviewInput struct {
	Targets    []Target `json:"targets"`
	Operation  string   `json:"operation"`
	Descriptor *Action  `json:"descriptor,omitempty"`
}
type PreviewRoute struct {
	Kind      string `json:"kind"`
	Mode      string `json:"mode"`
	Enabled   bool   `json:"enabled"`
	Permitted bool   `json:"permitted"`
	Reason    string `json:"reason"`
}
type PreviewRecipient struct {
	Target Target         `json:"target"`
	Name   string         `json:"name"`
	Routes []PreviewRoute `json:"routes"`
}
type DispatchPreview struct {
	Recipients       []PreviewRecipient `json:"recipients"`
	Count            int                `json:"count"`
	HiddenRecipients int                `json:"hidden_recipients"`
	Token            string             `json:"token"`
	ExpiresAt        time.Time          `json:"expires_at"`
	Sends            bool               `json:"sends"`
}
type dispatchToken struct {
	Scope, Actor, Input, Fingerprint string
	Until                            int64
}

// Explicit batches use individual identities or exact instances. Groups retain
// their normal group API and current-membership semantics.
func (s *Store) dispatchRecipients(ctx context.Context, db DB, a Actor, in DispatchPreviewInput) ([]recipient, error) {
	if len(in.Targets) == 1 && in.Targets[0].Kind == "group" {
		return s.resolve(ctx, db, a, in.Targets[0], in.Operation, "", in.Descriptor)
	}
	return s.batchRecipients(ctx, db, a, in)
}
func (s *Store) batchRecipients(ctx context.Context, db DB, a Actor, in DispatchPreviewInput) ([]recipient, error) {
	if len(in.Targets) == 0 || len(in.Targets) > 100 {
		return nil, Bad("Select 1 to 100 explicit targets.")
	}
	if in.Operation != "message" && in.Operation != "ask" {
		return nil, Bad("Operation must be message or ask.")
	}
	out := []recipient{}
	seen := map[string]string{}
	for _, t := range in.Targets {
		if t.Kind == "group" {
			return nil, Bad("Use the group API for group targets; batches select individual identities or instances.")
		}
		rs, e := s.resolve(ctx, db, a, t, in.Operation, "", in.Descriptor)
		if e != nil {
			return nil, e
		}
		for _, r := range rs {
			prior, ok := seen[r.ID]
			if ok {
				if prior != r.ExactInstance {
					return nil, Bad("Select a logical agent or one exact instance, without conflicting replica targets.")
				}
				continue
			}
			seen[r.ID] = r.ExactInstance
			out = append(out, r)
		}
	}
	return out, nil
}
func previewInputDigest(in DispatchPreviewInput) string {
	keys := map[string]Target{}
	for _, target := range in.Targets {
		keys[string(JSON(target))] = target
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	copy := in
	copy.Targets = []Target{}
	for _, key := range ordered {
		copy.Targets = append(copy.Targets, keys[key])
	}
	return digestJSON(copy)
}
func (s *Store) previewDispatch(ctx context.Context, db DB, a Actor, in DispatchPreviewInput) (DispatchPreview, string, error) {
	ctx = withPolicySnapshot(ctx)
	out := DispatchPreview{Recipients: []PreviewRecipient{}, ExpiresAt: time.Now().UTC().Add(10 * time.Minute)}
	rs, e := s.dispatchRecipients(ctx, db, a, in)
	if e != nil {
		return out, "", e
	}
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].ID != rs[j].ID {
			return rs[i].ID < rs[j].ID
		}
		return rs[i].ExactInstance < rs[j].ExactInstance
	})
	out.Count = len(rs)
	fingerprint := []any{}
	name := ""
	if in.Descriptor != nil {
		name = in.Descriptor.Name
	}
	ids := []string{}
	for _, r := range rs {
		ids = append(ids, r.ID)
	}
	type destination struct {
		did, mode, address, cid, kind string
		dv, cv                        int64
		priority                      int
		enabled                       bool
	}
	byRecipient := map[string][]destination{}
	rows, e := db.Query(ctx, `SELECT d.principal_id,d.id,d.version,d.mode,d.priority,d.address,c.id,c.version,c.kind,c.enabled FROM destinations d JOIN connections c ON c.scope=d.scope AND c.id=d.connection_id WHERE d.scope=$1 AND d.principal_id=ANY($2::text[]) ORDER BY d.priority,d.id`, a.Scope, ids)
	if e != nil {
		return out, "", e
	}
	for rows.Next() {
		var id string
		var d destination
		if e = rows.Scan(&id, &d.did, &d.dv, &d.mode, &d.priority, &d.address, &d.cid, &d.cv, &d.kind, &d.enabled); e != nil {
			rows.Close()
			return out, "", e
		}
		byRecipient[id] = append(byRecipient[id], d)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, "", e
	}
	for _, r := range rs {
		p := PreviewRecipient{Target: Target{r.Kind, r.ID}, Name: r.Name, Routes: []PreviewRoute{}}
		if r.ExactInstance != "" {
			p.Target = Target{"instance", r.ExactInstance}
		}
		native := s.allowed(ctx, db, a, in.Operation, r.PolicyTarget, "", name, in.Descriptor)
		p.Routes = append(p.Routes, PreviewRoute{Kind: "native", Mode: "primary", Enabled: true, Permitted: native, Reason: map[bool]string{true: "Current contact permission permits the durable inbox.", false: "Contact permission does not permit the native inbox."}[native]})
		fingerprint = append(fingerprint, []any{r.ID, r.ExactInstance, native})
		dests := byRecipient[r.ID]
		for _, d := range dests {
			allowed := s.allowed(ctx, db, a, in.Operation, r.PolicyTarget, d.cid, name, in.Descriptor)
			reason := "Eligible in addition to canonical history; provider acceptance has not been tested."
			if d.mode == "fallback" {
				reason = "Eligible only after a definite delivery failure."
			}
			if !d.enabled {
				reason = "Installation is disabled."
			} else if !allowed {
				reason = "Current contact permission excludes this installation."
			}
			p.Routes = append(p.Routes, PreviewRoute{d.kind, d.mode, d.enabled, allowed, reason})
			fingerprint = append(fingerprint, []any{r.ID, d.did, d.dv, d.mode, d.priority, d.address, d.cid, d.cv, d.enabled, allowed})
		}
		if a.ID == r.ID || s.allowed(ctx, db, a, "view", Target{r.Kind, r.ID}, "", "") {
			out.Recipients = append(out.Recipients, p)
		} else {
			out.HiddenRecipients++
		}
	}
	return out, digestJSON(fingerprint), nil
}
func (s *Store) DispatchPreview(ctx context.Context, a Actor, in DispatchPreviewInput) (DispatchPreview, error) {
	var out DispatchPreview
	err := s.tx(ctx, a, func(db pgx.Tx) error {
		var fingerprint string
		var e error
		out, fingerprint, e = s.previewDispatch(ctx, db, a, in)
		if e != nil {
			return e
		}
		raw, e := s.seal(JSON(dispatchToken{a.Scope, a.ID, previewInputDigest(in), fingerprint, out.ExpiresAt.Unix()}))
		if e == nil {
			out.Token = base64.RawURLEncoding.EncodeToString(raw)
		}
		return e
	})
	return out, err
}
func (s *Store) checkDispatchPreview(ctx context.Context, db DB, a Actor, in DispatchPreviewInput, token string) error {
	if token == "" {
		return nil
	}
	if len(token) > 4096 {
		return Bad("Invalid preview token.")
	}
	raw, e := base64.RawURLEncoding.DecodeString(token)
	if e != nil {
		return Bad("Invalid preview token.")
	}
	plain, e := s.unseal(raw)
	if e != nil {
		return Bad("Invalid preview token.")
	}
	var c dispatchToken
	if json.Unmarshal(plain, &c) != nil || c.Scope != a.Scope || c.Actor != a.ID || c.Input != previewInputDigest(in) || c.Until < time.Now().Unix() {
		return Conflict("Preview expired or does not match this dispatch. Preview again.")
	}
	_, hash, e := s.previewDispatch(ctx, db, a, in)
	if e != nil {
		return e
	}
	if c.Fingerprint != hash {
		return Conflict("Recipients or routes changed. Preview again before sending.")
	}
	return nil
}

type SendBatchInput struct {
	Targets      []Target `json:"targets"`
	Body         string   `json:"body"`
	Title        string   `json:"title"`
	PreviewToken string   `json:"preview_token,omitempty"`
}
type SendBatchItem struct {
	Target Target     `json:"target"`
	Result SendResult `json:"result"`
}
type AskBatchInput struct {
	Targets      []Target `json:"targets"`
	Request      AskInput `json:"request"`
	PreviewToken string   `json:"preview_token,omitempty"`
}
type AskBatchItem struct {
	Target  Target  `json:"target"`
	Request Request `json:"request"`
}

func (s *Store) SendBatch(ctx context.Context, a Actor, in SendBatchInput, key string) ([]SendBatchItem, error) {
	out := []SendBatchItem{}
	e := s.tx(ctx, a, func(db pgx.Tx) error {
		return idempotent(ctx, db, a, "send_batch", key, in, &out, func() error {
			preview := DispatchPreviewInput{Targets: in.Targets, Operation: "message"}
			rs, e := s.batchRecipients(ctx, db, a, preview)
			if e != nil {
				return e
			}
			if e = s.checkDispatchPreview(ctx, db, a, preview, in.PreviewToken); e != nil {
				return e
			}
			for _, r := range rs {
				t := Target{r.Kind, r.ID}
				if r.ExactInstance != "" {
					t = Target{"instance", r.ExactInstance}
				}
				var result SendResult
				if e = s.sendTx(ctx, db, a, SendInput{Target: t, Body: in.Body, Title: in.Title}, Digest(key+"/batch/"+r.ID), &result); e != nil {
					return e
				}
				out = append(out, SendBatchItem{t, result})
			}
			return nil
		})
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}
func (s *Store) AskBatch(ctx context.Context, a Actor, in AskBatchInput, key string) ([]AskBatchItem, error) {
	out := []AskBatchItem{}
	if in.Request.PreviewToken != "" || in.Request.ConversationID != "" || in.Request.Target.ID != "" || in.Request.Target.Kind != "" {
		return nil, Bad("Batch requests supply targets only in the targets list.")
	}
	e := s.tx(ctx, a, func(db pgx.Tx) error {
		return idempotent(ctx, db, a, "ask_batch", key, in, &out, func() error {
			preview := DispatchPreviewInput{Targets: in.Targets, Operation: "ask", Descriptor: in.Request.Action}
			rs, e := s.batchRecipients(ctx, db, a, preview)
			if e != nil {
				return e
			}
			if e = s.checkDispatchPreview(ctx, db, a, preview, in.PreviewToken); e != nil {
				return e
			}
			for _, r := range rs {
				t := Target{r.Kind, r.ID}
				if r.ExactInstance != "" {
					t = Target{"instance", r.ExactInstance}
				}
				q := in.Request
				q.Target = t
				var result Request
				if e = s.askTx(ctx, db, a, q, Digest(key+"/batch/"+r.ID), &result); e != nil {
					return e
				}
				out = append(out, AskBatchItem{t, result})
			}
			return nil
		})
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}
