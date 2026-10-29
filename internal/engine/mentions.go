package engine

import "context"

func (s *Store) validateMentions(ctx context.Context, db DB, a Actor, conv, body string, mentions []Mention) error {
	if len(mentions) > 20 {
		return Bad("Use at most 20 mentions in one message.")
	}
	// Membership and current visibility are checked by the same reader used for
	// message history. An address hint never invites a new participant.
	c, err := s.readConversationParticipants(ctx, db, a, conv)
	if err != nil {
		return err
	}
	members := map[string]Principal{}
	for _, p := range c.Participants {
		members[p.ID] = p
	}
	runes := []rune(body)
	last := 0
	for _, m := range mentions {
		p, ok := members[m.PrincipalID]
		if !ok || m.Start < last || m.End <= m.Start || m.End > len(runes) || string(runes[m.Start:m.End]) != "@"+p.Name {
			return Bad("Mentions must identify a current participant and match their text.")
		}
		last = m.End
	}
	return nil
}

// MentionContext is an optional runtime-facing attention hint. It preserves the
// canonical sender/body and never excludes observers or requires a response.
func MentionContext(message Message, recipient string) string {
	if len(message.Mentions) == 0 {
		return ""
	}
	for _, m := range message.Mentions {
		if m.PrincipalID == recipient {
			return "You are explicitly addressed. Other participants can also contribute relevant context."
		}
	}
	return "Other participants are explicitly addressed. You may contribute relevant context."
}
