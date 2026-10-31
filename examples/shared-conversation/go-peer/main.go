// Builder's side of the shared-conversation example, using an issued worker credential.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/beepgrid/beepgrid/sdk/go/notif"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	path := os.Getenv("NOTIF_TOKEN_FILE")
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("worker credential must be owner-only: chmod 600")
	}
	token, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	client := notif.New(os.Getenv("NOTIF_URL"), strings.TrimSpace(string(token)))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var input struct {
		Operation      string `json:"operation"`
		PersonID       string `json:"person_id"`
		ReviewerID     string `json:"reviewer_id"`
		DevOpsID       string `json:"devops_id"`
		ConversationID string `json:"conversation_id"`
		Key            string `json:"key"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		return err
	}
	if input.Operation == "start" {
		chat, err := client.StartChat(ctx, notif.StartChatInput{
			Targets: []notif.Target{{Kind: "agent", ID: input.ReviewerID}, {Kind: "person", ID: input.PersonID}},
			Title:   "Search API migration", Body: "Can we retire /v1/search?",
		}, input.Key+"/start")
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(chat)
	}
	if input.Operation != "invite" {
		return fmt.Errorf("choose start or invite")
	}
	raw, err := client.Conversation(ctx, input.ConversationID)
	if err != nil {
		return err
	}
	var history struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &history); err != nil {
		return err
	}
	// Adding someone discloses history only because this example opts in explicitly.
	if _, err := client.ChangeChatMembership(ctx, input.ConversationID, notif.ChatMembershipInput{
		Operation: "add", Version: history.Version, Members: []string{input.DevOpsID}, ShareHistory: true,
	}, input.Key+"/invite"); err != nil {
		return err
	}
	body := "Looping in @DevOps to move the two remaining jobs."
	message, err := client.Send(ctx, notif.SendInput{
		ConversationID: input.ConversationID, Body: body,
		Mentions: []notif.Mention{{PrincipalID: input.DevOpsID, Start: 11, End: 18}},
	}, input.Key+"/loop-in")
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(message)
}
