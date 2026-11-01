package engine

import (
	"context"
	"reflect"
	"testing"
)

func TestMessageReceiptsRequireNativeAckAndRemainSenderOnly(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "receipt-worker")
	sent, err := s.Send(ctx, owner, SendInput{Target: Target{"agent", worker.ID}, Body: "Receipt review"}, "receipt-message")
	if err != nil {
		t.Fatal(err)
	}
	check := func(actor Actor, want []string) {
		t.Helper()
		chat, err := s.Conversation(ctx, actor, sent.ConversationID)
		if err != nil || len(chat.Messages) != 1 || !reflect.DeepEqual(chat.Messages[0].ReceivedBy, want) {
			t.Fatalf("conversation receipts: %#v, %v; want %v", chat.Messages, err, want)
		}
		page, err := s.Query(ctx, actor, QueryInput{Collection: "messages", Filters: map[string]string{"conversation_id": sent.ConversationID}})
		if err != nil || len(page.Items) != 1 || !reflect.DeepEqual(page.Items[0].(Message).ReceivedBy, want) {
			t.Fatalf("paged receipts: %#v, %v; want %v", page, err, want)
		}
		single, err := s.queryRecord(ctx, actor, "messages", sent.Messages[0].ID)
		if err != nil || !reflect.DeepEqual(single.(Message).ReceivedBy, want) {
			t.Fatalf("single receipts: %#v, %v; want %v", single, err, want)
		}
	}
	check(owner, []string{})
	deliveries, err := s.Inbox(ctx, worker)
	if err != nil || len(deliveries) != 1 {
		t.Fatal(deliveries, err)
	}
	d := deliveries[0]
	// A lease without its current generation's acknowledgement is not receipt.
	check(owner, []string{})
	if err = s.Ack(ctx, worker, d.ID, d.Generation+1); ErrorStatus(err) != 409 {
		t.Fatal("stale generation acknowledged", err)
	}
	check(owner, []string{})
	if err = s.Ack(ctx, worker, d.ID, d.Generation); err != nil {
		t.Fatal(err)
	}
	check(owner, []string{worker.ID})
	check(worker, []string{})
	events, err := s.Events(ctx, owner, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Kind == "worker.received" && event.ConversationID == sent.ConversationID {
			found = true
		}
	}
	if !found {
		t.Fatal("native acknowledgement did not identify the message conversation")
	}
	// A provider's acceptance/receipt must never produce the native tick.
	if _, err = s.Pool.Exec(ctx, `UPDATE deliveries SET connection_id='provider-fixture' WHERE scope=$1 AND id=$2`, owner.Scope, d.ID); err != nil {
		t.Fatal(err)
	}
	check(owner, []string{})
	if _, err = s.Pool.Exec(ctx, `UPDATE deliveries SET connection_id='' WHERE scope=$1 AND id=$2`, owner.Scope, d.ID); err != nil {
		t.Fatal(err)
	}
	check(owner, []string{worker.ID})
	outsider := testPerson(t, s, owner, "receipt-outsider")
	if _, err = s.Conversation(ctx, outsider, sent.ConversationID); ErrorStatus(err) != 404 {
		t.Fatal("receipt exposed private chat", err)
	}
}
