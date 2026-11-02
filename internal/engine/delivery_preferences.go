package engine

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

// ChannelPolicy chooses among the Person's existing destinations. It grants no
// contact or provider authority. Empty destinations means workspace history only.
type ChannelPolicy struct {
	Mode            string   `json:"mode"`
	Destinations    []string `json:"destinations"`
	FollowupSeconds int      `json:"followup_seconds"`
}
type ChannelOverride struct {
	Source Target        `json:"source"` // agent, or group with purpose agent_role
	Policy ChannelPolicy `json:"policy"`
}
type DeliveryPreferences struct {
	PersonID  string            `json:"person_id"`
	Version   int64             `json:"version"`
	Default   *ChannelPolicy    `json:"default"`
	Overrides []ChannelOverride `json:"overrides"` // First matching Agent role wins.
}

func (s *Store) mayEditDeliveryPreferences(ctx context.Context, db DB, a Actor, id string) bool {
	p, err := principal(ctx, db, a.Scope, id)
	return err == nil && p.Kind == "person" && (a.Admin || a.Kind == "person" && a.ID == id || s.managesIdentity(ctx, db, a, p))
}
func readDeliveryPreferences(ctx context.Context, db DB, scope, id string) (DeliveryPreferences, error) {
	out := DeliveryPreferences{PersonID: id, Overrides: []ChannelOverride{}}
	var def, overrides []byte
	err := db.QueryRow(ctx, `SELECT version,default_policy,overrides FROM delivery_preferences WHERE scope=$1 AND person_id=$2`, scope, id).Scan(&out.Version, &def, &overrides)
	if err == pgx.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if len(def) > 0 {
		if err = json.Unmarshal(def, &out.Default); err != nil {
			return out, err
		}
	}
	err = json.Unmarshal(overrides, &out.Overrides)
	return out, err
}
func (s *Store) DeliveryPreferences(ctx context.Context, a Actor, id string) (DeliveryPreferences, error) {
	var out DeliveryPreferences
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		if !s.mayEditDeliveryPreferences(ctx, tx, a, id) {
			return Denied()
		}
		var err error
		out, err = readDeliveryPreferences(ctx, tx, a.Scope, id)
		return err
	})
	return out, err
}
func (s *Store) SaveDeliveryPreferences(ctx context.Context, a Actor, id string, in DeliveryPreferences) (DeliveryPreferences, error) {
	in.PersonID = id
	if len(in.Overrides) > 64 {
		return in, Bad("Choose at most 64 Agent or Agent role overrides.")
	}
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		if !s.mayEditDeliveryPreferences(ctx, tx, a, id) {
			return Denied()
		}
		previous, err := readDeliveryPreferences(ctx, tx, a.Scope, id)
		if err != nil {
			return err
		}
		if in.Version != previous.Version {
			return Conflict("Delivery preferences changed. Reload before saving.")
		}
		validate := func(p ChannelPolicy) error {
			if p.Mode != "preferred" && p.Mode != "all" {
				return Bad("Choose preferred channels or all selected channels.")
			}
			if len(p.Destinations) > 16 {
				return Bad("Choose at most 16 destinations.")
			}
			if p.FollowupSeconds != 0 && (p.FollowupSeconds < 60 || p.FollowupSeconds > 604800) {
				return Bad("Follow-up delay must be between one minute and seven days.")
			}
			if p.Mode == "all" && p.FollowupSeconds != 0 {
				return Bad("All selected channels do not need timed follow-ups.")
			}
			seen := map[string]bool{}
			for _, dest := range p.Destinations {
				if seen[dest] {
					return Bad("A destination can appear only once.")
				}
				seen[dest] = true
				var own bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM destinations WHERE scope=$1 AND id=$2 AND principal_id=$3)`, a.Scope, dest, id).Scan(&own); err != nil {
					return err
				}
				if !own {
					return Bad("Choose an existing destination for this Person.")
				}
			}
			return nil
		}
		if in.Default != nil {
			if err = validate(*in.Default); err != nil {
				return err
			}
		}
		seen := map[Target]bool{}
		for _, o := range in.Overrides {
			if seen[o.Source] {
				return Bad("Choose one override per Agent or Agent role.")
			}
			seen[o.Source] = true
			var valid bool
			switch o.Source.Kind {
			case "agent":
				err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM principals WHERE scope=$1 AND id=$2 AND kind='agent' AND NOT disabled)`, a.Scope, o.Source.ID).Scan(&valid)
			case "group":
				err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM groups WHERE scope=$1 AND id=$2 AND purpose='agent_role')`, a.Scope, o.Source.ID).Scan(&valid)
			default:
				return Bad("An override must select an Agent or Agent role.")
			}
			if err != nil {
				return err
			}
			visible := a.Admin || s.allowed(ctx, tx, a, "view", o.Source, "", "") || s.manages(ctx, tx, a, o.Source.Kind, o.Source.ID)
			if valid && !visible && o.Source.Kind == "agent" {
				p, e := principal(ctx, tx, a.Scope, o.Source.ID)
				visible = e == nil && s.inspectIdentity(ctx, tx, a, p)
			}
			if valid && !visible && o.Source.Kind == "group" {
				if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE scope=$1 AND group_id=$2 AND principal_id=$3)`, a.Scope, o.Source.ID, a.ID).Scan(&visible); err != nil {
					return err
				}
			}
			if !valid || !visible {
				return Denied()
			}
			if err = validate(o.Policy); err != nil {
				return err
			}
		}
		if in.Overrides == nil {
			in.Overrides = []ChannelOverride{}
		}
		var def any
		if in.Default != nil {
			def = JSON(in.Default)
		}
		err = tx.QueryRow(ctx, `INSERT INTO delivery_preferences(scope,person_id,default_policy,overrides) VALUES($1,$2,$3,$4)
   ON CONFLICT(scope,person_id) DO UPDATE SET default_policy=EXCLUDED.default_policy,overrides=EXCLUDED.overrides,version=delivery_preferences.version+1 RETURNING version`, a.Scope, id, def, JSON(in.Overrides)).Scan(&in.Version)
		if err != nil {
			return err
		}
		// Stop pending external sends from the old policy. In-flight network sends
		// cannot be recalled. Canonical history and native worker delivery stay intact.
		if _, err = tx.Exec(ctx, `UPDATE deliveries SET state='cancelled',last_error='Recipient delivery preferences changed' WHERE scope=$1 AND recipient_id=$2 AND connection_id<>'' AND routing->>'managed'='true' AND state IN ('queued','standby','waiting')`, a.Scope, id); err != nil {
			return err
		}
		return event(ctx, tx, a, "delivery_preferences.saved", "", id, map[string]any{"person_id": id, "version": in.Version})
	})
	return in, err
}
