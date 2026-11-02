package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"testing"

	"github.com/beepgrid/beepgrid/internal/engine"
	"github.com/beepgrid/beepgrid/sdk/go"
)

func TestHTTPDraftFirstMessageAndMembership(t *testing.T) {
	s, admin, server := setup(t)
	s.AttachmentsRoot = t.TempDir()
	ctx := context.Background()
	actor, err := s.Authenticate(ctx, admin.Token)
	if err != nil {
		t.Fatal(err)
	}
	workers := []engine.Actor{}
	var lastToken string
	for _, name := range []string{"Builder 1", "Billing 2", "Reviewer 3"} {
		session, e := s.Enroll(ctx, actor, engine.EnrollInput{Agent: name, Name: name})
		if e != nil {
			t.Fatal(e)
		}
		role, e := s.SaveRole(ctx, actor, engine.Role{Name: name, Rules: []engine.Rule{{Action: "*", TargetKind: "*", TargetID: "*"}}})
		if e != nil {
			t.Fatal(e)
		}
		if e = s.BindRole(ctx, actor, engine.Binding{RoleID: role.ID, SubjectKind: "principal", SubjectID: session.Agent.ID}, false); e != nil {
			t.Fatal(e)
		}
		lastToken = session.Token
		a, e := s.Authenticate(ctx, session.Token)
		if e != nil {
			t.Fatal(e)
		}
		workers = append(workers, a)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="review.txt"`)
	header.Set("Content-Type", "text/plain")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte("Specific checkout migration evidence."))
	writer.Close()
	req, err := http.NewRequest("POST", server.URL+"/api/v1/attachments", &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+admin.Token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
	var file engine.Attachment
	if err = json.NewDecoder(response.Body).Decode(&file); err != nil {
		t.Fatal(err)
	}
	var sent engine.SendResult
	if err = admin.Do(ctx, "POST", "/api/v1/conversations", engine.StartChatInput{Targets: []engine.Target{{Kind: "agent", ID: workers[0].ID}, {Kind: "agent", ID: workers[1].ID}}, Body: "", AttachmentIDs: []string{file.ID}}, &sent, "first-file"); err != nil {
		t.Fatal(err)
	}
	var changed engine.ChatMembershipResult
	input := engine.ChatMembershipInput{Operation: "add", Version: 1, Members: []string{workers[2].ID}}
	if err = admin.Do(ctx, "POST", "/api/v1/conversations/"+sent.ConversationID+"/membership", input, &changed, "add-member"); err != nil || changed.Version != 2 {
		t.Fatal(changed, err)
	}
	// Actor-bound HTTP reads get the same history fence as engine readers.
	newcomer := beepgrid.New(server.URL, lastToken)
	var chat engine.Conversation
	if err = newcomer.Do(ctx, "GET", "/api/v1/conversations/"+sent.ConversationID, nil, &chat, ""); err != nil || len(chat.Messages) != 0 {
		t.Fatal(chat, err)
	}
	if _, err = newcomer.DownloadAttachment(ctx, file.ID); err == nil {
		t.Fatal("pre-join file exposed over HTTP")
	}
	var replay engine.ChatMembershipResult
	if err = admin.Do(ctx, "POST", "/api/v1/conversations/"+sent.ConversationID+"/membership", input, &replay, "add-member"); err != nil || replay.Version != 2 {
		t.Fatal(replay, err)
	}
}
