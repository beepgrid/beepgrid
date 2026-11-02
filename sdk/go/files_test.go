package beepgrid

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAttachmentBinaryContractAndRedirectRejection(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("missing auth")
		}
		if r.Method == "POST" {
			if err := r.ParseMultipartForm(65536); err != nil {
				t.Error(err)
				return
			}
			defer r.MultipartForm.RemoveAll()
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			defer file.Close()
			raw, _ := io.ReadAll(file)
			if string(raw) != "%PDF-fixture" || header.Filename != "brief.pdf" || header.Header.Get("Content-Type") != "application/pdf" {
				t.Error("incorrect upload", header, string(raw))
			}
			fmt.Fprint(w, `{"id":"att_1","file_name":"brief.pdf"}`)
		} else {
			w.Header().Set("Content-Type", "application/pdf")
			fmt.Fprint(w, "%PDF-fixture")
		}
	}))
	defer server.Close()
	c := New(server.URL, "fixture")
	ctx := context.Background()
	file, err := c.UploadAttachment(ctx, "con_1", "brief.pdf", "application/pdf", []byte("%PDF-fixture"))
	if err != nil || file.ID != "att_1" {
		t.Fatal(file, err)
	}
	raw, err := c.DownloadAttachment(ctx, file.ID)
	if err != nil || string(raw) != "%PDF-fixture" || calls != 2 {
		t.Fatal(string(raw), err, calls)
	}
	if _, err = c.UploadAttachment(ctx, "con_1", "brief\r\n.pdf", "application/pdf", []byte("x")); err == nil {
		t.Fatal("header injection accepted")
	}
	leak := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("followed attachment redirect") }))
	defer leak.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, leak.URL, http.StatusFound) }))
	defer redirect.Close()
	if _, err = New(redirect.URL, "fixture").DownloadAttachment(ctx, "att_1"); err == nil || strings.Contains(err.Error(), "fixture") {
		t.Fatal(err)
	}
}

func TestRealFilesEventsAndDirectory(t *testing.T) {
	base := os.Getenv("BEEPGRID_SDK_TEST_URL")
	if base == "" {
		t.Skip("Requires real HTTP")
	}
	owner := New(base, os.Getenv("BEEPGRID_SDK_TEST_TOKEN"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	worker, session, err := owner.Enroll(ctx, EnrollInput{Agent: "sdk/go-files-" + Key(), Name: "Go file worker"})
	if err != nil {
		t.Fatal(err)
	}
	var me struct {
		Person Person `json:"person"`
	}
	if err = owner.Do(ctx, "GET", "/api/v1/me", nil, &me, ""); err != nil {
		t.Fatal(err)
	}
	var role struct {
		ID string `json:"id"`
	}
	if err = owner.Do(ctx, "POST", "/api/v1/roles", map[string]any{"name": "Go file reader " + Key(), "rules": []map[string]string{{"action": "view", "target_kind": "person", "target_id": me.Person.ID}}}, &role, ""); err != nil {
		t.Fatal(err)
	}
	if err = owner.Do(ctx, "POST", "/api/v1/role-bindings", map[string]string{"role_id": role.ID, "subject_kind": "principal", "subject_id": session.Agent.ID}, nil, ""); err != nil {
		t.Fatal(err)
	}
	sent, err := owner.Send(ctx, SendInput{Target: Target{Kind: "agent", ID: session.Agent.ID}, Body: "Go file review"}, Key())
	if err != nil {
		t.Fatal(err)
	}
	file, err := owner.UploadAttachment(ctx, sent.ConversationID, "brief.pdf", "application/pdf", []byte("%PDF-1.4\nfixture"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := worker.DownloadAttachment(ctx, file.ID)
	if err != nil || string(data) != "%PDF-1.4\nfixture" {
		t.Fatal(string(data), err)
	}
	if files, err := worker.Attachments(ctx, sent.ConversationID); err != nil || len(files) != 1 || files[0].ID != file.ID {
		t.Fatal(files, err)
	}
	if people, err := owner.Directory(ctx, "person", ""); err != nil || len(people.Items) == 0 {
		t.Fatal(people, err)
	}
	if os.Getenv("BEEPGRID_SDK_EXPECT_START_CHAT_MENTIONS") != "" {
		body := "👋 @Go file worker please review"
		mentions := []Mention{{PrincipalID: session.Agent.ID, Start: 2, End: 2 + len([]rune("@Go file worker"))}}
		shared, err := owner.StartChat(ctx, StartChatInput{Targets: []Target{{Kind: "agent", ID: session.Agent.ID}}, Body: body, Mentions: mentions}, Key())
		if err != nil || len(shared.Messages) != 1 || len(shared.Messages[0].Mentions) != 1 || shared.Messages[0].Mentions[0] != mentions[0] {
			t.Fatal(shared, err)
		}
	}
	done := errors.New("applied checkpoint")
	err = worker.StreamEvents(ctx, EventOptions{ConversationID: sent.ConversationID}, func(event Event) error {
		if event.Kind == "message.created" {
			if !strings.Contains(string(event.Data), "Go file review") {
				t.Error(string(event.Data))
			}
			return done
		}
		return nil
	})
	if err != done {
		t.Fatal(err)
	}
}
