package engine

import (
	"context"
	"sync"
	"testing"
)

func TestDirectHistoryActivityAndPrivateBatches(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	one := testWorker(t, s, owner, "Builder 1")
	two := testWorker(t, s, owner, "Builder 2")
	first, e := s.Send(ctx, owner, SendInput{Target: Target{"agent", one.ID}, Body: "First message", ReuseDirect: true}, "direct-1")
	if e != nil {
		t.Fatal(e)
	}
	second, e := s.Send(ctx, owner, SendInput{Target: Target{"agent", two.ID}, Body: "Another chat", ReuseDirect: true}, "direct-2")
	if e != nil {
		t.Fatal(e)
	}
	reply, e := s.Send(ctx, one, SendInput{Target: Target{"person", owner.ID}, Body: "The migration is ready", ReuseDirect: true}, "reply")
	if e != nil || reply.ConversationID != first.ConversationID {
		t.Fatal(reply, e)
	}
	page, e := s.Query(ctx, owner, QueryInput{Collection: "conversations", Limit: 1, Filters: map[string]string{"participant_id": owner.ID}})
	if e != nil || len(page.Items) != 1 {
		t.Fatal(page, e)
	}
	top := page.Items[0].(Conversation)
	if top.ID != first.ConversationID || top.LastPreview != "The migration is ready" || page.Order != "last_activity_at_id_descending" {
		t.Fatal(top, page.Order)
	}
	next, e := s.Query(ctx, owner, QueryInput{Collection: "conversations", Limit: 1, Filters: map[string]string{"participant_id": owner.ID}, Cursor: page.NextCursor})
	if e != nil || len(next.Items) != 1 || next.Items[0].(Conversation).ID != second.ConversationID {
		t.Fatal(next, e)
	}
	req, e := s.Ask(ctx, owner, AskInput{Target: Target{"agent", one.ID}, Kind: "approval", Body: "Restart staging?", ReuseDirect: true}, "approval")
	if e != nil || req.ConversationID != first.ConversationID {
		t.Fatal(req, e)
	}
	_, e = s.Respond(ctx, one, req.ID, Response{Version: req.Version, Outcome: "approved"})
	if e != nil {
		t.Fatal(e)
	}
	c, e := s.Conversation(ctx, owner, first.ConversationID)
	if e != nil || c.LastPreview != "approved" {
		t.Fatal(c, e)
	}
	batch, e := s.SendBatch(ctx, owner, SendBatchInput{Targets: []Target{{"agent", one.ID}, {"agent", two.ID}}, Body: "Private update"}, "private-batch")
	if e != nil || len(batch) != 2 {
		t.Fatal(batch, e)
	}
	if batch[0].Result.ConversationID == batch[1].Result.ConversationID {
		t.Fatal("private batch shared history")
	}
	for _, item := range batch {
		c, e := s.Conversation(ctx, owner, item.Result.ConversationID)
		if e != nil || len(c.Participants) != 2 {
			t.Fatal(c, e)
		}
	}
	replay, e := s.SendBatch(ctx, owner, SendBatchInput{Targets: []Target{{"agent", one.ID}, {"agent", two.ID}}, Body: "Private update"}, "private-batch")
	if e != nil || replay[0].Result.ConversationID != batch[0].Result.ConversationID {
		t.Fatal(replay, e)
	}
	stranger := testPerson(t, s, owner, "Unrelated reader")
	leaked, e := s.Query(ctx, stranger, QueryInput{Collection: "conversations"})
	if e != nil || len(leaked.Items) != 0 {
		t.Fatal("activity preview leaked", leaked, e)
	}
}
func TestConcurrentDirectHistoryReuse(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	one := testWorker(t, s, owner, "Builder")
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, e := s.Send(ctx, owner, SendInput{Target: Target{"agent", one.ID}, Body: "Concurrent message", ReuseDirect: true}, ID("key"))
			if e != nil {
				errs <- e
			} else {
				ids <- result.ConversationID
			}
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	id := ""
	for got := range ids {
		if id != "" && id != got {
			t.Fatal("created duplicate direct threads", id, got)
		}
		id = got
	}
	c, e := s.Conversation(ctx, owner, id)
	if e != nil || len(c.Messages) != 8 {
		t.Fatal(c, e)
	}
}
