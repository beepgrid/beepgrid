package engine

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

type assistanceActorKey struct{}

type overseerHistoryKey struct{}

// OverseerTurn is bounded, person-authorized conversation context, never authority.
type OverseerTurn struct {
	Prompt       string          `json:"prompt"`
	Summary      string          `json:"summary"`
	Operation    string          `json:"operation"`
	Body         string          `json:"body,omitempty"`
	Dispatched   bool            `json:"dispatched"`
	ReplySummary string          `json:"reply_summary,omitempty"`
	Replies      []OverseerReply `json:"replies"`
}

type OverseerInput struct {
	PreviousPlanID string  `json:"previous_plan_id,omitempty"`
	Prompt         string  `json:"prompt"`
	Target         *Target `json:"target,omitempty"`
}
type OverseerDecision struct {
	Operation string   `json:"operation"`
	AgentIDs  []string `json:"agent_ids"`
	Body      string   `json:"body,omitempty"`
	Summary   string   `json:"summary"`
}
type OverseerPlanner func(context.Context, string, []FleetStatus) (OverseerDecision, error)
type OverseerReply struct {
	AgentID string `json:"agent_id"`
	Name    string `json:"name"`
	State   string `json:"state"`
	Outcome string `json:"outcome,omitempty"`
	Answer  any    `json:"answer,omitempty"`
}
type OverseerSummarizer func(context.Context, string, []OverseerReply) (string, error)
type OverseerDispatch struct {
	AgentID        string `json:"agent_id"`
	ConversationID string `json:"conversation_id"`
	RequestID      string `json:"request_id,omitempty"`
}
type OverseerPlan struct {
	ScopeAgentIDs        []string            `json:"scope_agent_ids,omitempty"`
	ContextGroups        map[string][]string `json:"context_groups,omitempty"`
	ContextRequestIDs    []string            `json:"context_request_ids,omitempty"`
	PreviousPlanID       string              `json:"previous_plan_id,omitempty"`
	ID                   string              `json:"id"`
	Prompt               string              `json:"prompt"`
	ContextIDs           []string            `json:"context_agent_ids"`
	Target               *Target             `json:"target,omitempty"`
	Operation            string              `json:"operation"`
	Summary              string              `json:"summary"`
	Body                 string              `json:"body,omitempty"`
	Sources              []FleetStatus       `json:"sources"`
	RequiresConfirmation bool                `json:"requires_confirmation"`
	CreatedAt            time.Time           `json:"created_at"`
	ExpiresAt            time.Time           `json:"expires_at"`
	Dispatches           []OverseerDispatch  `json:"dispatches"`
	ReplySummary         string              `json:"reply_summary,omitempty"`
	ReplyDigest          string              `json:"reply_digest,omitempty"`
	Replies              []Request           `json:"replies"`
}

func featureDisabled() error {
	return &Error{"FEATURE_DISABLED", "Enable this optional feature in workspace settings first.", 409}
}
func (s *Store) overseerAccess(ctx context.Context, db DB, a Actor) (AssistanceSettings, error) {
	settings, err := s.assistanceSettings(ctx, db, a.Scope)
	if err != nil {
		return settings, err
	}
	if !settings.Overseer {
		return settings, featureDisabled()
	}
	person, e := principal(ctx, db, a.Scope, a.ID)
	if e != nil || person.Disabled || a.Kind != "person" {
		return settings, Denied()
	}
	return settings, nil
}

// PlanOverseer exposes only permitted structured fleet information to the provider.
// Providers propose bounded operations; they never receive credentials or execute tools.
func (s *Store) PlanOverseer(ctx context.Context, a Actor, in OverseerInput, keys ...string) (OverseerPlan, error) {
	var out OverseerPlan
	if !ValidText(in.Prompt, 4000) || len(in.PreviousPlanID) > 200 {
		return out, Bad("Ask a question or describe an instruction in at most 4000 bytes.")
	}
	if s.OverseerPlanner == nil {
		return out, Conflict("The Coordinator provider is unavailable.")
	}
	var settings AssistanceSettings
	var sources []FleetStatus
	var history []OverseerTurn
	key := ""
	if len(keys) > 0 && keys[0] != "" {
		key = keys[0]
		if !ValidText(key, 200) {
			return out, Bad("Invalid Idempotency-Key.")
		}
	}
	replay := false
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		var err error
		settings, err = s.overseerAccess(ctx, tx, a)
		if err != nil {
			return err
		}
		if key != "" {
			var hash string
			var receipt []byte
			e := tx.QueryRow(ctx, `SELECT input_hash,output FROM idempotency WHERE scope=$1 AND principal_id=$2 AND operation='overseer_plan' AND key=$3`, a.Scope, a.ID, key).Scan(&hash, &receipt)
			if e == nil {
				if hash != digestJSON(in) {
					return Conflict("The idempotency key was already used with different input.")
				}
				if e = json.Unmarshal(receipt, &out); e != nil {
					return e
				}
				out, e = s.overseerPlan(ctx, tx, a, out.ID)
				replay = e == nil
				return e
			}
			if !errors.Is(e, pgx.ErrNoRows) {
				return e
			}
		}
		history, err = s.overseerHistory(ctx, tx, a, in.PreviousPlanID)
		if err != nil {
			return err
		}
		fleet, err := s.fleetStatus(ctx, tx, a, in.Target)
		if err == nil {
			for _, source := range fleet["agents"].([]FleetStatus) {
				if !source.Agent.Disabled {
					sources = append(sources, source)
				}
			}
		}
		return err
	})
	if err != nil {
		return out, err
	}
	if replay {
		return out, nil
	}
	if len(sources) == 0 {
		return out, Missing()
	}
	if len(JSON(sources)) > 24576 {
		return out, Bad("Select a smaller fleet for this question.")
	}
	historyDigest := digestJSON(history)
	// Leave room for gateway instructions and envelope fields inside its 32 KiB bound.
	for len(JSON(sources))+len(JSON(in.Prompt))+len(JSON(history)) > 30000 && len(history) > 1 {
		history = history[1:]
	}
	if len(JSON(sources))+len(JSON(in.Prompt))+len(JSON(history)) > 30000 {
		return out, Bad("Select a smaller fleet or start a new topic for this follow-up.")
	}
	bounded, cancel := context.WithTimeout(context.WithValue(context.WithValue(ctx, assistanceActorKey{}, a.Scope+"/"+a.ID), overseerHistoryKey{}, history), 30*time.Second)
	defer cancel()
	decision, err := s.OverseerPlanner(bounded, in.Prompt, sources)
	if err != nil {
		return out, &Error{"ASSISTANCE_UNAVAILABLE", "Coordinator could not prepare an answer. No instructions were sent.", 503}
	}
	if decision.Operation != "status" && decision.Operation != "message" && decision.Operation != "question" && decision.Operation != "task" {
		return out, Bad("Coordinator proposed an unsupported operation.")
	}
	if !ValidText(decision.Summary, 4000) || len(decision.AgentIDs) > 100 {
		return out, Bad("The Coordinator response exceeds its bounds.")
	}
	if decision.Operation != "status" && (!ValidText(decision.Body, 4000) || len(decision.AgentIDs) == 0) {
		return out, Bad("An outbound plan requires explicit agents and a bounded message.")
	}
	byID := map[string]FleetStatus{}
	for _, source := range sources {
		byID[source.Agent.ID] = source
	}
	selected := []FleetStatus{}
	seen := map[string]bool{}
	if len(decision.AgentIDs) == 0 {
		decision.AgentIDs = make([]string, 0, len(sources))
		for _, source := range sources {
			decision.AgentIDs = append(decision.AgentIDs, source.Agent.ID)
		}
	}
	for _, id := range decision.AgentIDs {
		source, ok := byID[id]
		if !ok {
			return out, Denied()
		}
		if !seen[id] {
			selected = append(selected, source)
			seen[id] = true
		}
	}
	out = OverseerPlan{ID: ID("ovr"), Prompt: in.Prompt, Operation: decision.Operation, Summary: decision.Summary, Body: decision.Body, Sources: selected, RequiresConfirmation: decision.Operation != "status", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(10 * time.Minute), Dispatches: []OverseerDispatch{}, Replies: []Request{}}
	out.Target = in.Target
	out.PreviousPlanID = in.PreviousPlanID
	for _, source := range sources {
		out.ContextIDs = append(out.ContextIDs, source.Agent.ID)
		out.ScopeAgentIDs = append(out.ScopeAgentIDs, source.Agent.ID)
	}
	if in.PreviousPlanID != "" {
		parent, e := s.OverseerPlan(ctx, a, in.PreviousPlanID)
		if e != nil {
			return OverseerPlan{}, e
		}
		known := map[string]bool{}
		for _, id := range out.ContextIDs {
			known[id] = true
		}
		for _, id := range parent.ContextIDs {
			if !known[id] {
				out.ContextIDs = append(out.ContextIDs, id)
				known[id] = true
			}
		}
		out.ContextGroups = parent.ContextGroups
		if out.ContextGroups == nil {
			out.ContextGroups = map[string][]string{}
		}
		if parent.Target != nil && parent.Target.Kind == "group" {
			ids := parent.ScopeAgentIDs
			if len(ids) == 0 {
				ids = parent.ContextIDs
			}
			out.ContextGroups[parent.Target.ID] = append(out.ContextGroups[parent.Target.ID], ids...)
		}
		out.ContextRequestIDs = append(out.ContextRequestIDs, parent.ContextRequestIDs...)
		for _, reply := range parent.Replies {
			out.ContextRequestIDs = append(out.ContextRequestIDs, reply.ID)
		}
		unique := func(ids []string) []string {
			seen := map[string]bool{}
			result := []string{}
			for _, id := range ids {
				if !seen[id] {
					seen[id] = true
					result = append(result, id)
				}
			}
			return result
		}
		out.ContextRequestIDs = unique(out.ContextRequestIDs)
		for group, ids := range out.ContextGroups {
			out.ContextGroups[group] = unique(ids)
		}
		// Bound accumulated provenance too. A fresh topic starts without inherited context.
		if len(out.ContextIDs) > 200 || len(out.ContextGroups) > 16 || len(out.ContextRequestIDs) > 100 {
			return OverseerPlan{}, Bad("Start a new topic to keep conversation context bounded.")
		}

	}
	err = s.tx(ctx, a, func(tx pgx.Tx) error {
		current, err := s.overseerAccess(ctx, tx, a)
		if err != nil {
			return err
		}
		if current.Version != settings.Version {
			return Conflict("Assistance settings changed. Ask again.")
		}
		for _, source := range sources {
			if _, err = s.resolve(ctx, tx, a, Target{"agent", source.Agent.ID}, "view", ""); err != nil {
				return err
			}
		}
		for _, source := range selected {
			if _, err = s.resolve(ctx, tx, a, Target{"agent", source.Agent.ID}, "view", ""); err != nil {
				return err
			}
			if out.Operation != "status" {
				action := "message"
				if out.Operation != "message" {
					action = "ask"
				}
				if _, err = s.resolve(ctx, tx, a, Target{"agent", source.Agent.ID}, action, ""); err != nil {
					return err
				}
			}
		}
		if err = s.validateOverseerContext(ctx, tx, a, out); err != nil {
			return err
		}
		freshHistory, err := s.overseerHistory(ctx, tx, a, in.PreviousPlanID)
		if err != nil {
			return err
		}
		if historyDigest != digestJSON(freshHistory) {
			return Conflict("Conversation context changed. Ask again.")
		}
		save := func() error {
			if _, err = tx.Exec(ctx, `INSERT INTO overseer_plans(id,scope,person_id,version,plan,expires_at) VALUES($1,$2,$3,$4,$5,$6)`, out.ID, a.Scope, a.ID, current.Version, JSON(out), out.ExpiresAt); err != nil {
				return err
			}
			return event(ctx, tx, a, "overseer.planned", "", a.ID, map[string]any{"id": out.ID, "operation": out.Operation, "agent_count": len(selected)})
		}
		if key != "" {
			return idempotent(ctx, tx, a, "overseer_plan", key, in, &out, save)
		}
		return save()
	})
	if err != nil {
		return OverseerPlan{}, err
	}
	return s.OverseerPlan(ctx, a, out.ID)
}

// Read a short chain using the same private, current-policy reader as the UI.
func (s *Store) overseerHistory(ctx context.Context, db DB, a Actor, id string) ([]OverseerTurn, error) {
	turns := []OverseerTurn{}
	seen := map[string]bool{}
	for id != "" && len(turns) < 6 {
		if seen[id] {
			return nil, Bad("Invalid conversation context.")
		}
		seen[id] = true
		p, err := s.overseerPlan(ctx, db, a, id)
		if err != nil {
			return nil, err
		}
		turn := OverseerTurn{Prompt: p.Prompt, Summary: p.Summary, Operation: p.Operation, Body: p.Body, Dispatched: len(p.Dispatches) > 0, ReplySummary: p.ReplySummary, Replies: []OverseerReply{}}
		for _, r := range p.Replies {
			reply := OverseerReply{State: r.State}
			for _, d := range p.Dispatches {
				if d.RequestID == r.ID {
					reply.AgentID = d.AgentID
				}
			}
			for _, source := range p.Sources {
				if source.Agent.ID == reply.AgentID {
					reply.Name = source.Agent.Name
				}
			}
			if r.Response != nil {
				reply.Outcome = r.Response.Outcome
				reply.Answer = r.Response.Answer
			}
			turn.Replies = append(turn.Replies, reply)
		}
		if len(JSON(append(turns, turn))) > 12288 {
			if len(turns) == 0 {
				return nil, Bad("This reply is too large for follow-up context. Start a new topic.")
			}
			break
		}
		turns = append(turns, turn)
		id = p.PreviousPlanID
	}
	for i, j := 0, len(turns)-1; i < j; i, j = i+1, j-1 {
		turns[i], turns[j] = turns[j], turns[i]
	}
	return turns, nil
}

func (s *Store) readOverseerPlan(ctx context.Context, db DB, a Actor, id string) (OverseerPlan, int64, bool, error) {
	var out OverseerPlan
	var raw, result []byte
	var version int64
	err := db.QueryRow(ctx, `SELECT plan,version,result FROM overseer_plans WHERE scope=$1 AND id=$2 AND person_id=$3`, a.Scope, id, a.ID).Scan(&raw, &version, &result)
	if err != nil {
		return out, 0, false, normalize(err)
	}
	if err = json.Unmarshal(raw, &out); err != nil {
		return out, 0, false, err
	}
	committed := len(result) > 0
	if committed {
		if err = json.Unmarshal(result, &out.Dispatches); err != nil {
			return out, 0, false, err
		}
		out.RequiresConfirmation = false
	}
	if err = s.validateOverseerContext(ctx, db, a, out); err != nil {
		return OverseerPlan{}, 0, false, err
	}

	return out, version, committed, nil
}
func (s *Store) validateOverseerContext(ctx context.Context, db DB, a Actor, out OverseerPlan) error {
	ctx = withPolicySnapshot(ctx)
	for _, id := range out.ContextRequestIDs {
		if _, err := s.request(ctx, db, a, id); err != nil {
			return Missing()
		}
	}
	for group, ids := range out.ContextGroups {
		current, err := s.resolve(ctx, db, a, Target{"group", group}, "view", "")
		if err != nil {
			return Missing()
		}
		members := map[string]bool{}
		for _, member := range current {
			members[member.ID] = true
		}
		for _, id := range ids {
			if !members[id] {
				return Missing()
			}
		}
	}
	// Context IDs are canonical IDs generated from the current fleet, never
	// external aliases. Load that exact bounded set and still recheck each grant.
	people, err := principalsByIDs(ctx, db, a.Scope, out.ContextIDs)
	if err != nil {
		return err
	}
	for _, id := range out.ContextIDs {
		p, ok := people[id]
		if !ok || p.Kind != "agent" || p.Disabled || !s.allowed(ctx, db, a, "view", Target{"agent", id}, "*", "") {
			return Missing()
		}
	}
	if out.Target != nil && out.Target.Kind == "group" {
		current, err := s.resolve(ctx, db, a, *out.Target, "view", "")
		if err != nil {
			return Missing()
		}
		members := map[string]bool{}
		for _, member := range current {
			members[member.ID] = true
		}
		ids := out.ScopeAgentIDs
		if len(ids) == 0 {
			ids = out.ContextIDs
		}
		for _, sourceID := range ids {
			if !members[sourceID] {
				return Missing()
			}
		}
	}
	return nil
}
func (s *Store) overseerPlan(ctx context.Context, db DB, a Actor, id string) (OverseerPlan, error) {
	if _, err := s.overseerAccess(ctx, db, a); err != nil {
		return OverseerPlan{}, err
	}
	out, _, _, err := s.readOverseerPlan(ctx, db, a, id)
	if err != nil {
		return out, err
	}
	out.Replies = []Request{}
	for _, dispatch := range out.Dispatches {
		if dispatch.RequestID != "" {
			r, err := s.request(ctx, db, a, dispatch.RequestID)
			if err != nil {
				return OverseerPlan{}, err
			}
			out.Replies = append(out.Replies, r)
		}
	}
	if err = db.QueryRow(ctx, `SELECT reply_summary,reply_digest FROM overseer_plans WHERE scope=$1 AND id=$2 AND person_id=$3`, a.Scope, id, a.ID).Scan(&out.ReplySummary, &out.ReplyDigest); err != nil {
		return OverseerPlan{}, err
	}
	if out.ReplyDigest != digestJSON(out.Replies) {
		out.ReplySummary = ""
		out.ReplyDigest = ""
	}
	return out, nil
}
func (s *Store) OverseerPlan(ctx context.Context, a Actor, id string) (OverseerPlan, error) {
	var out OverseerPlan
	err := s.tx(ctx, a, func(tx pgx.Tx) error { var err error; out, err = s.overseerPlan(ctx, tx, a, id); return err })
	return out, err
}
func (s *Store) OverseerPlans(ctx context.Context, a Actor) ([]OverseerPlan, error) {
	out := []OverseerPlan{}
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		if _, err := s.overseerAccess(ctx, tx, a); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id FROM overseer_plans WHERE scope=$1 AND person_id=$2 ORDER BY created_at DESC,id DESC LIMIT 20`, a.Scope, a.ID)
		if err != nil {
			return err
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range ids {
			plan, err := s.overseerPlan(ctx, tx, a, id)
			if err != nil {
				if ErrorStatus(err) == 404 || ErrorStatus(err) == 403 {
					continue
				}
				return err
			}
			out = append(out, plan)
		}
		return nil
	})
	return out, err
}

// SummarizeOverseer reads replies as the original person. No service identity or
// executable tools are exposed to the provider, and changed access invalidates output.
func (s *Store) SummarizeOverseer(ctx context.Context, a Actor, id string) (OverseerPlan, error) {
	var out OverseerPlan
	var version int64
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		var err error
		settings, err := s.overseerAccess(ctx, tx, a)
		if err != nil {
			return err
		}
		version = settings.Version
		out, err = s.overseerPlan(ctx, tx, a, id)
		return err
	})
	if err != nil {
		return OverseerPlan{}, err
	}
	if len(out.Replies) == 0 {
		return out, Bad("This plan has no agent replies to summarize.")
	}
	if out.ReplySummary != "" {
		return out, nil
	}
	if s.OverseerSummarizer == nil {
		return out, Conflict("The Coordinator provider is unavailable.")
	}
	replies := []OverseerReply{}
	for i, r := range out.Replies {
		reply := OverseerReply{AgentID: out.Dispatches[i].AgentID, State: r.State}
		for _, source := range out.Sources {
			if source.Agent.ID == reply.AgentID {
				reply.Name = source.Agent.Name
			}
		}
		if r.Response != nil {
			reply.Outcome = r.Response.Outcome
			reply.Answer = r.Response.Answer
		}
		replies = append(replies, reply)
	}
	if len(JSON(replies)) > 24576 {
		return out, Bad("The replies exceed the summary context limit. Read the individual responses.")
	}
	digest := digestJSON(out.Replies)
	bounded, cancel := context.WithTimeout(context.WithValue(ctx, assistanceActorKey{}, a.Scope+"/"+a.ID), 30*time.Second)
	defer cancel()
	summary, err := s.OverseerSummarizer(bounded, out.Prompt, replies)
	if err != nil {
		return OverseerPlan{}, &Error{"ASSISTANCE_UNAVAILABLE", "The replies remain available, but the summary could not be prepared.", 503}
	}
	if !ValidText(summary, 4000) {
		return OverseerPlan{}, Bad("The summary exceeds its bounds.")
	}
	err = s.tx(ctx, a, func(tx pgx.Tx) error {
		settings, err := s.overseerAccess(ctx, tx, a)
		if err != nil {
			return err
		}
		if version != settings.Version {
			return Conflict("Assistance settings changed. Ask again.")
		}
		out, err = s.overseerPlan(ctx, tx, a, id)
		if err != nil {
			return err
		}
		if digest != digestJSON(out.Replies) {
			return Conflict("Agent replies changed. Refresh before summarizing.")
		}
		if _, err = tx.Exec(ctx, `UPDATE overseer_plans SET reply_summary=$4,reply_digest=$5 WHERE scope=$1 AND id=$2 AND person_id=$3`, a.Scope, id, a.ID, summary, digest); err != nil {
			return err
		}
		out.ReplySummary = summary
		out.ReplyDigest = digest
		return event(ctx, tx, a, "overseer.summarized", "", a.ID, map[string]any{"id": id, "reply_count": len(replies)})
	})
	if err != nil {
		return OverseerPlan{}, err
	}
	return out, nil
}

// ConfirmOverseer atomically sends every approved instruction through the existing
// policy, idempotency and durable transport paths, under the original person's identity.
func (s *Store) ConfirmOverseer(ctx context.Context, a Actor, id string) (OverseerPlan, error) {
	var out OverseerPlan
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		settings, err := s.overseerAccess(ctx, tx, a)
		if err != nil {
			return err
		}
		var version int64
		var committed bool
		out, version, committed, err = s.readOverseerPlan(ctx, tx, a, id)
		if err != nil {
			return err
		}
		if committed {
			return nil
		}
		if version != settings.Version || time.Now().After(out.ExpiresAt) {
			return Conflict("This preview expired or settings changed. Ask again.")
		}
		if out.Operation == "status" {
			return Bad("A status answer has no instructions to confirm.")
		}
		for _, source := range out.Sources {
			target := Target{"agent", source.Agent.ID}
			delivery := OverseerDispatch{AgentID: target.ID}
			if out.Operation == "message" {
				var sent SendResult
				if err = s.sendTx(ctx, tx, a, SendInput{Target: target, Title: "Coordinator: " + strings.ToValidUTF8(out.Prompt[:min(len(out.Prompt), 150)], ""), Body: out.Body}, out.ID+"/"+target.ID, &sent); err != nil {
					return err
				}
				delivery.ConversationID = sent.ConversationID
			} else {
				var request Request
				if err = s.askTx(ctx, tx, a, AskInput{Target: target, Kind: out.Operation, Title: "Coordinator: " + strings.ToValidUTF8(out.Prompt[:min(len(out.Prompt), 150)], ""), Body: out.Body}, out.ID+"/"+target.ID, &request); err != nil {
					return err
				}
				delivery.RequestID = request.ID
				delivery.ConversationID = request.ConversationID
			}
			out.Dispatches = append(out.Dispatches, delivery)
		}
		if _, err = tx.Exec(ctx, `UPDATE overseer_plans SET result=$4 WHERE scope=$1 AND id=$2 AND person_id=$3`, a.Scope, id, a.ID, JSON(out.Dispatches)); err != nil {
			return err
		}
		out.RequiresConfirmation = false
		return event(ctx, tx, a, "overseer.confirmed", "", a.ID, map[string]any{"id": id, "operation": out.Operation, "agent_count": len(out.Dispatches)})
	})
	if err != nil {
		return OverseerPlan{}, err
	}
	return out, nil
}
