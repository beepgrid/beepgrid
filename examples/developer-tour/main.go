// Complete Go client tour. Use only a disposable workspace's owner credential.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	notif "github.com/beepgrid/beepgrid/sdk/go/notif"
	"os"
	"strings"
	"time"
)

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}
func check(ok bool) {
	if !ok {
		panic("unexpected API result")
	}
}
func decoded(raw json.RawMessage) map[string]any {
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(err)
	}
	return out
}

func main() {
	// region setup
	ctx := context.Background()
	credential := os.Getenv("NOTIF_TOKEN_FILE")
	info := must(os.Stat(credential))
	check(info.Mode().Perm()&0077 == 0)
	token := must(os.ReadFile(credential))
	owner := notif.New(os.Getenv("NOTIF_URL"), strings.TrimSpace(string(token)))
	runID := notif.Key()
	var me struct {
		Person struct {
			ID string `json:"id"`
		} `json:"person"`
	}
	if err := owner.Do(ctx, "GET", "/api/v1/me", nil, &me, ""); err != nil {
		panic(err)
	}
	builder, builderSession, err := owner.Enroll(ctx, notif.EnrollInput{Agent: "tour/" + runID + "/builder", Name: "Builder 7"})
	if err != nil {
		panic(err)
	}
	billing, billingSession, err := owner.Enroll(ctx, notif.EnrollInput{Agent: "tour/" + runID + "/billing", Name: "Billing 2"})
	if err != nil {
		panic(err)
	}
	builderID, billingID := builderSession.Agent.ID, billingSession.Agent.ID
	for _, pair := range [][2]string{{builderID, billingID}, {billingID, builderID}} {
		rules := []map[string]any{}
		for _, contact := range [][2]string{{"person", me.Person.ID}, {"agent", pair[1]}} {
			for _, action := range []string{"view", "message", "ask"} {
				rules = append(rules, map[string]any{"action": action, "target_kind": contact[0], "target_id": contact[1]})
			}
		}
		var role struct {
			ID string `json:"id"`
		}
		if err := owner.Do(ctx, "POST", "/api/v1/roles", map[string]any{"name": runID + "/" + pair[0], "rules": rules}, &role, ""); err != nil {
			panic(err)
		}
		if err := owner.Do(ctx, "POST", "/api/v1/role-bindings", map[string]any{"role_id": role.ID, "subject_kind": "principal", "subject_id": pair[0]}, nil, ""); err != nil {
			panic(err)
		}
	}
	target := notif.Target{Kind: "agent", ID: billingID}
	targets := []notif.Target{{Kind: "agent", ID: builderID}, target}
	// endregion setup

	// region messages
	check(len(must(builder.Directory(ctx, "agent", "")).Items) > 0)
	sent := must(builder.Send(ctx, notif.SendInput{Target: target, Body: "Check the payment ledger for orders 241 and 242."}, runID+"/message"))
	must(billing.Send(ctx, notif.SendInput{ConversationID: sent.ConversationID, Body: "Both payments settled yesterday."}, notif.Key()))
	check(len(decoded(must(builder.Conversation(ctx, sent.ConversationID)))["messages"].([]any)) == 2)
	receiptInput := notif.SendInput{Target: target, Body: "The reconciliation is complete."}
	receipt := must(builder.Notify(ctx, receiptInput, runID+"/receipt"))
	check(must(builder.Send(ctx, receiptInput, runID+"/receipt")).ConversationID == receipt.ConversationID)
	preview := must(owner.DispatchPreview(ctx, notif.DispatchPreviewInput{Targets: targets, Operation: "message"}))
	batch := must(owner.SendBatch(ctx, notif.SendBatchInput{Targets: targets, Body: "Hold the staging rollout until checkout jobs drain.", PreviewToken: preview.Token}, runID+"/batch"))
	check(len(batch) == 2 && batch[0].Result.ConversationID != batch[1].Result.ConversationID)
	// endregion messages
	if os.Getenv("NOTIF_TOUR_MESSAGES_ONLY") == "1" {
		fmt.Println("PASS: Go first exchange")
		return
	}

	// region requests
	request := must(builder.Ask(ctx, notif.AskInput{Target: target, Body: "Which region contains order 241?", Choices: []string{"EU", "US"}}, runID+"/ask"))
	current := must(billing.GetRequest(ctx, request.ID))
	must(billing.Respond(ctx, request.ID, notif.Response{Outcome: "answered", Version: current.Version, Answer: "EU"}))
	waitCtx, waitCancel := context.WithTimeout(ctx, 5*time.Second)
	check(must(builder.Wait(waitCtx, request.ID)).Response.Answer == "EU")
	waitCancel()
	pending := must(builder.Ask(ctx, notif.AskInput{Target: target, Body: "Check order 243 as well."}, notif.Key()))
	revised := must(builder.ReplaceRequest(ctx, pending.ID, notif.ReplaceRequestInput{Version: pending.Version, Body: "Check orders 243 and 244."}, runID+"/replace")).Replacement
	check(must(builder.Cancel(ctx, revised.ID, revised.Version)).State == "cancelled")
	check(len(must(owner.AskBatch(ctx, notif.AskBatchInput{Targets: targets, Request: notif.AskInput{Body: "Is staging available?"}}, runID+"/ask-batch"))) == 2)
	blocking := must(builder.Ask(ctx, notif.AskInput{Target: target, Body: "I need the ledger before continuing."}, notif.Key()))
	check(must(builder.SetRequestBlocking(ctx, blocking.ID, true)).Blocking)
	must(builder.SetRequestBlocking(ctx, blocking.ID, false))
	// endregion requests

	// region chats
	chat := must(owner.StartChat(ctx, notif.StartChatInput{Targets: targets, Body: "👋 @Billing 2 check these orders.", Mentions: []notif.Mention{{PrincipalID: billingID, Start: 2, End: 12}}}, runID+"/chat"))
	check(chat.Messages[0].AttentionFor(billingID) != "")
	history := decoded(must(owner.Conversation(ctx, chat.ConversationID)))
	version := int(history["version"].(float64))
	changed := must(owner.ChangeChatMembership(ctx, chat.ConversationID, notif.ChatMembershipInput{Operation: "rename", Version: version, Title: "Checkout reconciliation"}, runID+"/rename"))
	check(changed.Version > version)
	// endregion chats

	// region files
	pdf := []byte("%PDF-1.4\nPayment reconciliation fixture\n%%EOF")
	draft := must(builder.StageAttachment(ctx, "orders.pdf", "application/pdf", pdf))
	withFile := must(builder.Send(ctx, notif.SendInput{Target: target, Body: "Please check these payment records.", AttachmentIDs: []string{draft.ID}}, notif.Key()))
	check(must(billing.Attachments(ctx, withFile.ConversationID))[0].ID == draft.ID)
	check(string(must(billing.DownloadAttachment(ctx, draft.ID))) == string(pdf))
	check(must(builder.UploadAttachment(ctx, withFile.ConversationID, "notes.txt", "text/plain", []byte("Orders 241 and 242"))).ID != draft.ID)
	// endregion files

	// region queries
	input := notif.QueryInput{Collection: "messages", Filters: map[string]string{"conversation_id": sent.ConversationID}, Limit: 1}
	page := must(owner.Query(ctx, input))
	seen := len(page.Items)
	for page.NextCursor != "" {
		input.Cursor = page.NextCursor
		page = must(owner.Query(ctx, input))
		seen += len(page.Items)
	}
	check(seen == 2)
	check(must(owner.Capabilities(ctx, notif.CapabilityInput{RequestID: blocking.ID})).Operations["cancel"])
	check(decoded(must(owner.Access(ctx, "")))["roles"] != nil)
	check(decoded(must(owner.RequestTrace(ctx, blocking.ID, "")))["request"] != nil)
	check(decoded(must(owner.Workspace(ctx)))["deployment"] == "self_hosted")
	check(decoded(must(owner.Account(ctx)))["applied"] != nil)
	check(len(must(owner.ProviderCatalog(ctx))) > 0)
	var connection struct {
		ID string `json:"id"`
	}
	if err := owner.Do(ctx, "POST", "/api/v1/connections", map[string]any{"name": "Tour webhook " + runID, "kind": "webhook", "enabled": false, "config": map[string]string{"url": "https://example.com/beepgrid"}, "secret": notif.Key()}, &connection, ""); err != nil {
		panic(err)
	}
	check(len(must(owner.ConnectionSetup(ctx, connection.ID))) > 0)
	// endregion queries

	// region workers
	if err := billing.Heartbeat(ctx, nil); err != nil {
		panic(err)
	}
	for _, delivery := range must(billing.Inbox(ctx)) {
		if err := billing.Ack(ctx, delivery); err != nil {
			panic(err)
		}
	}
	must(builder.Send(ctx, notif.SendInput{Target: target, Body: "A final worker event."}, notif.Key()))
	workerCtx, cancelWorker := context.WithTimeout(ctx, 500*time.Millisecond)
	processed := 0
	err = billing.Run(workerCtx, func(_ context.Context, delivery notif.Delivery) error {
		check(delivery.ID != "")
		processed++
		return nil
	})
	check(err != nil && workerCtx.Err() != nil && processed > 0)
	cancelWorker()
	// endregion workers

	// region events
	streamCtx, cancelStream := context.WithTimeout(ctx, 5*time.Second)
	found := false
	err = billing.StreamEvents(streamCtx, notif.EventOptions{}, func(event notif.Event) error {
		if event.Kind == "message.created" && event.ConversationID == sent.ConversationID {
			check(event.Sequence > 0)
			found = true
			cancelStream()
		}
		return nil // Persist the checkpoint only after applying the event.
	})
	check(found && err != nil)
	cancelStream()
	// endregion events

	// region execution
	action := notif.Action{Name: "reconcile-orders", ExecutorID: builderID, Resource: "orders", ResourceVersion: "v2", Arguments: map[string]any{"ids": []int{241, 242}}, OperationKey: runID + "/business"}
	approval := must(owner.Ask(ctx, notif.AskInput{Target: notif.Target{Kind: "person", ID: me.Person.ID}, Body: "Apply this reconciliation?", Kind: "approval", Action: &action}, notif.Key()))
	must(owner.Respond(ctx, approval.ID, notif.Response{Outcome: "approved", Version: approval.Version}))
	execution := must(builder.Redeem(ctx, approval.ID, action))
	check(must(builder.ReportExecution(ctx, execution.ID, "succeeded", map[string]bool{"fixture": true})).State == "succeeded")
	// endregion execution

	// region coordinator
	settings := must(owner.Assistance(ctx))
	check(settings.OverseerConfigured)
	enabled := true
	must(owner.UpdateAssistance(ctx, notif.AssistanceUpdate{Overseer: &enabled, Version: settings.Version}))
	plan := must(owner.PlanOverseer(ctx, notif.OverseerInput{Prompt: "Ask the available specialists whether staging is ready."}, notif.Key()))
	check(must(owner.OverseerPlan(ctx, plan.ID)).ID == plan.ID)
	check(len(must(owner.OverseerPlans(ctx))) > 0)
	check(must(owner.ConfirmOverseer(ctx, plan.ID)).ID == plan.ID)
	time.Sleep(1100 * time.Millisecond)
	check(must(owner.SummarizeOverseer(ctx, plan.ID)).ID == plan.ID)
	time.Sleep(1100 * time.Millisecond)
	check(must(owner.Coordinate(ctx, notif.OverseerInput{Prompt: "Ask the specialists to report any release blockers."}, runID+"/coordinate")).ID != "")
	// endregion coordinator
	fmt.Println("PASS: Go developer tour")
}
