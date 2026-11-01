package engine

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

// Capabilities are observations, never bearer grants. A mutation always checks
// current policy independently. Descriptor-specific rules need that descriptor.
type CapabilityInput struct {
	Target         Target  `json:"target"`
	ConversationID string  `json:"conversation_id,omitempty"`
	RequestID      string  `json:"request_id,omitempty"`
	ConnectionID   string  `json:"connection_id,omitempty"`
	Descriptor     *Action `json:"descriptor,omitempty"`
}
type CapabilityResult struct {
	ObservedAt     time.Time       `json:"observed_at"`
	Operations     map[string]bool `json:"operations"`
	RequestVersion int             `json:"request_version,omitempty"`
	Outcomes       []string        `json:"outcomes"`
}

func (s *Store) Capabilities(ctx context.Context, a Actor, in CapabilityInput) (CapabilityResult, error) {
	out := CapabilityResult{ObservedAt: time.Now().UTC(), Operations: map[string]bool{}, Outcomes: []string{}}
	err := s.tx(ctx, a, func(db pgx.Tx) error {
		ctx := withPolicySnapshot(ctx)
		if in.RequestID != "" {
			r, e := s.request(ctx, db, a, in.RequestID)
			if e != nil {
				return e
			}
			out.RequestVersion = r.Version
			out.Operations["view"] = true
			out.Operations["cancel"] = r.State == "open" && (a.Admin || r.RequesterID == a.ID)
			out.Operations["supersede"] = out.Operations["cancel"]
			out.Operations["replace"] = r.State == "open" && r.RequesterID == a.ID
			out.Operations["respond"] = r.State == "open" && s.responseAuthorization(ctx, db, a, r) == nil
			if out.Operations["respond"] {
				switch r.Kind {
				case "approval":
					out.Outcomes = []string{"approved", "denied"}
				case "question":
					out.Outcomes = []string{"answered"}
				case "task":
					out.Outcomes = []string{"completed", "failed", "rejected"}
				}
			}
			return nil
		}
		if in.ConversationID != "" {
			if !s.canRead(ctx, db, a, in.ConversationID) {
				return Missing()
			}
			out.Operations["view"] = true
			for _, operation := range []string{"message", "ask"} {
				_, e := s.conversationRecipients(ctx, db, a, in.ConversationID, operation, in.Descriptor)
				out.Operations[operation] = e == nil
			}
			return nil
		}

		t := in.Target
		if t.Kind == "" && t.ID == "" {
			for _, kind := range []string{"person", "service", "group", "role", "connection"} {
				out.Operations["create_"+kind] = s.manages(ctx, db, a, kind, "*")
			}
			out.Operations["enroll_agent"] = a.Admin
			out.Operations["configure_assistance"] = a.Kind == "person" && s.manages(ctx, db, a, "assistance", "workspace")
			out.Operations["publish_answer"] = s.canPublish(ctx, db, a)
			return nil
		}
		if t.ID == "" {
			return Bad("A resource ID is required.")
		}
		actionName := ""
		if in.Descriptor != nil {
			actionName = in.Descriptor.Name
		}
		switch t.Kind {
		case "person", "agent", "service":
			p, e := principal(ctx, db, a.Scope, t.ID)
			if e != nil || p.Kind != t.Kind || !s.inspectIdentity(ctx, db, a, p) {
				return Missing()
			}
			out.Operations["view"] = !p.Disabled && s.allowed(ctx, db, a, "view", t, in.ConnectionID, actionName, in.Descriptor)
			out.Operations["manage"] = s.managesIdentity(ctx, db, a, p)
			out.Operations["view_contact_access"] = s.inspectAccess(ctx, db, a, p)
			out.Operations["issue_credential"] = !p.Disabled && s.mayIssueCredential(ctx, db, a, p)
			out.Operations["link_account"] = p.Kind == "person" && !p.Disabled && s.mayLinkAccount(ctx, db, a, p.ID)
			if in.ConnectionID != "" {
				out.Operations["manage_destination"] = s.mayManageDestination(ctx, db, a, in.ConnectionID, p.ID)
			}
		case "group":
			var exists, member bool
			e := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM groups WHERE scope=$1 AND id=$2),EXISTS(SELECT 1 FROM memberships WHERE scope=$1 AND group_id=$2 AND principal_id=$3)`, a.Scope, t.ID, a.ID).Scan(&exists, &member)
			if e != nil {
				return e
			}
			if !exists || !(member || s.manages(ctx, db, a, "group", t.ID) || s.allowed(ctx, db, a, "view", t, "", "")) {
				return Missing()
			}
			out.Operations["view"] = true
			out.Operations["manage"] = s.manages(ctx, db, a, "group", t.ID)
		case "role":
			var r Role
			var raw []byte
			if e := db.QueryRow(ctx, `SELECT id,name,rules,version FROM roles WHERE scope=$1 AND id=$2`, a.Scope, t.ID).Scan(&r.ID, &r.Name, &raw, &r.Version); e != nil {
				return normalize(e)
			}
			if !s.roleVisible(ctx, db, a, r.ID) {
				return Missing()
			}
			if e := json.Unmarshal(raw, &r.Rules); e != nil {
				return e
			}
			out.Operations["view"] = true
			out.Operations["manage"] = s.mayEditRole(ctx, db, a, r)
			// Assignment also requires a separately manageable subject at mutation time.
			out.Operations["assign_to_managed_subject"] = s.manages(ctx, db, a, "role_binding", r.ID)
		case "connection":
			var id string
			if e := db.QueryRow(ctx, `SELECT id FROM connections WHERE scope=$1 AND id=$2`, a.Scope, t.ID).Scan(&id); e != nil {
				return normalize(e)
			}
			managed := s.mayManageConnection(ctx, db, a, id)
			if !managed && !s.allowed(ctx, db, a, "view", t, "", "") {
				return Missing()
			}
			out.Operations["view"] = true
			out.Operations["manage"] = managed
		case "instance":
			var pid string
			if e := db.QueryRow(ctx, `SELECT agent_id FROM instances WHERE scope=$1 AND id=$2`, a.Scope, t.ID).Scan(&pid); e != nil {
				return normalize(e)
			}
			if !s.allowed(ctx, db, a, "view", Target{"agent", pid}, "", "") {
				return Missing()
			}
			out.Operations["view"] = true
		case "destination":
			var connection, pid string
			if e := db.QueryRow(ctx, `SELECT connection_id,principal_id FROM destinations WHERE scope=$1 AND id=$2`, a.Scope, t.ID).Scan(&connection, &pid); e != nil {
				return normalize(e)
			}
			managed := s.mayManageDestination(ctx, db, a, connection, pid)
			if !a.Admin && a.ID != pid && !managed && !s.allowed(ctx, db, a, "view", Target{"destination", pid}, "", "") {
				return Missing()
			}
			out.Operations["view"] = true
			out.Operations["manage"] = managed
		case "delivery":
			d, e := deliveryScan(db.QueryRow(ctx, `SELECT `+deliveryFields+` FROM deliveries WHERE scope=$1 AND id=$2`, a.Scope, t.ID))
			if e != nil {
				return normalize(e)
			}
			if !s.mayReadDelivery(ctx, db, a, d) {
				return Missing()
			}
			out.Operations["view"] = true
			out.Operations["retry"] = (a.Admin || s.manages(ctx, db, a, "delivery", d.ID)) && (d.State == "failed" || d.State == "unknown") && d.ExpiresAt.After(time.Now()) && s.currentDeliveryPolicy(ctx, db, a.Scope, d)
		case "answer":
			x, e := scanAnswer(db.QueryRow(ctx, `SELECT `+answerFields+` FROM shared_answers WHERE scope=$1 AND id=$2`, a.Scope, t.ID))
			if e != nil {
				return normalize(e)
			}
			managed := a.Admin || s.manages(ctx, db, a, "answer", t.ID) || (x.PublisherID == a.ID && s.canPublish(ctx, db, a))
			if !managed && (!contains(x.Audience, a.ID) || x.Invalidated || !x.ValidUntil.After(time.Now()) || !s.publisherCurrent(ctx, db, a.Scope, x)) {
				return Missing()
			}
			out.Operations["view"] = true
			out.Operations["invalidate"] = managed && !x.Invalidated
		default:
			return Bad("Unsupported capability resource.")
		}
		out.Operations["inspect"] = true
		if t.Kind == "person" || t.Kind == "agent" || t.Kind == "service" || t.Kind == "group" || t.Kind == "instance" {
			for _, op := range []string{"message", "ask"} {
				_, e := s.resolve(ctx, db, a, t, op, actionName, in.Descriptor)
				out.Operations[op] = e == nil
			}
			out.Operations["decide"] = s.allowed(ctx, db, a, "decide", t, in.ConnectionID, actionName, in.Descriptor)
		}
		return nil
	})
	return out, err
}

type EffectiveRole struct {
	Role        Role   `json:"role"`
	SubjectKind string `json:"subject_kind"`
	SubjectID   string `json:"subject_id"`
}
type AccessSummary struct {
	Subject    Principal       `json:"subject"`
	Basis      string          `json:"basis"`
	Roles      []EffectiveRole `json:"roles"`
	ObservedAt time.Time       `json:"observed_at"`
}

func (s *Store) inspectAccess(ctx context.Context, db DB, a Actor, p Principal) bool {
	return a.Admin || a.ID == p.ID || (s.inspectIdentity(ctx, db, a, p) && s.allowed(ctx, db, a, "view", Target{"access", p.ID}, "", ""))
}
func (s *Store) Access(ctx context.Context, a Actor, subject string) (AccessSummary, error) {
	if subject == "" {
		subject = a.ID
	}
	out := AccessSummary{Basis: "effective_role_grants", Roles: []EffectiveRole{}, ObservedAt: time.Now().UTC()}
	err := s.tx(ctx, a, func(db pgx.Tx) error {
		p, e := principal(ctx, db, a.Scope, subject)
		if e != nil || !s.inspectAccess(ctx, db, a, p) {
			return Missing()
		}
		out.Subject = p
		if a.Admin && subject == a.ID {
			out.Basis = "administrator_credential"
		}
		rows, e := db.Query(ctx, `SELECT r.id,r.name,r.rules,r.version,b.subject_kind,b.subject_id FROM role_bindings b JOIN roles r ON r.scope=b.scope AND r.id=b.role_id WHERE b.scope=$1 AND ((b.subject_kind='principal' AND b.subject_id=$2) OR (b.subject_kind='group' AND EXISTS(SELECT 1 FROM memberships m WHERE m.scope=b.scope AND m.group_id=b.subject_id AND m.principal_id=$2))) ORDER BY r.id,b.subject_kind,b.subject_id`, a.Scope, subject)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var x EffectiveRole
			var raw []byte
			if e = rows.Scan(&x.Role.ID, &x.Role.Name, &raw, &x.Role.Version, &x.SubjectKind, &x.SubjectID); e != nil {
				return e
			}
			if e = json.Unmarshal(raw, &x.Role.Rules); e != nil {
				return e
			}
			out.Roles = append(out.Roles, x)
		}
		return rows.Err()
	})
	return out, err
}

// Metadata for workspace permission controls. It lists implemented resource
// categories and identifiers, rather than suggesting arbitrary tool permissions.
func PermissionCatalog() map[string]any {
	return map[string]any{
		"actions":      []string{"view", "message", "ask", "decide", "manage"},
		"rule_effects": []string{"allow", "deny"},
		"explicit_contact_blocks_override_grants": true,
		"request_scoped_replies":                  true,
		"communication_targets":                   []string{"person", "agent", "service", "group", "instance"},
		"management_targets":                      map[string]string{"person": "Person ID or explicit collection *", "agent": "Agent ID", "service": "Service ID or explicit collection *", "group": "Group ID or explicit collection *", "group_creation": "workspace, with current managed-member bounds", "role": "Role ID or explicit collection *", "role_binding": "Role ID plus separately manageable assignment subject", "credential": "Owning identity ID", "account": "Person ID for linking; view account:workspace for account status", "connection": "Installation ID or explicit collection *", "destination": "Recipient identity ID plus separately managed installation", "answer": "Publisher identity ID for publication, or answer ID for invalidation", "assistance": "workspace; Person administration only", "delivery": "Delivery ID with separate visibility and current route policy"},
		"policy_inspection":                       "view access:<subject ID> plus identity visibility; self access is readable",
		"conditions":                              []string{"resource", "resource_version", "currency", "max_amount_minor", "before"},
		"conditions_apply_to":                     []string{"ask", "decide"},
		"administrator_credential_bypass":         true,
		"manage_does_not_grant_business_view":     true,
		"grants_tool_execution":                   false,
	}
}
