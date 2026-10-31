package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/beepgrid/beepgrid/sdk/go/notif"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	token := os.Getenv("NOTIF_TOKEN")
	if file := os.Getenv("NOTIF_TOKEN_FILE"); file != "" {
		info, err := os.Stat(file)
		if err != nil {
			panic(err)
		}
		if info.Mode().Perm()&0077 != 0 {
			panic("credential file must be owner-only (chmod 600)")
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			panic(err)
		}
		token = strings.TrimSpace(string(raw))
	}
	if token == "" {
		panic("set NOTIF_TOKEN_FILE or NOTIF_TOKEN")
	}
	c := notif.New(value("NOTIF_URL", "http://localhost:4010"), token)
	var identity struct {
		Kind       string      `json:"identity_type"`
		Agent      notif.Agent `json:"agent"`
		InstanceID string      `json:"instance_id"`
	}
	if agent := strings.TrimSpace(os.Getenv("NOTIF_AGENT_ID")); agent != "" {
		worker, session, err := c.Enroll(ctx, notif.EnrollInput{Agent: agent, Name: value("NOTIF_AGENT_NAME", "Go worker"), Capabilities: []string{"message"}})
		if err != nil {
			panic(err)
		}
		c = worker
		identity.Kind, identity.Agent, identity.InstanceID = "agent", session.Agent, session.InstanceID
	} else {
		if err := c.Do(ctx, "GET", "/api/v1/me", nil, &identity, ""); err != nil {
			panic(err)
		}
	}
	if identity.Kind != "agent" || identity.Agent.ID == "" || identity.InstanceID == "" {
		panic("without NOTIF_AGENT_ID, supply an existing worker credential for an agent connection")
	}
	fmt.Println("Agent:", identity.Agent.ID, "Instance:", identity.InstanceID)
	err := c.Run(ctx, func(ctx context.Context, d notif.Delivery) error {
		if d.Payload.Type == "message" && d.Payload.Message != nil {
			_, e := c.Send(ctx, notif.SendInput{ConversationID: d.Payload.Message.ConversationID, Body: "Go worker received: " + d.Payload.Message.Body}, "reply/"+d.ID)
			return e
		}
		return nil
	})
	if err != nil && ctx.Err() == nil {
		panic(err)
	}
}

func value(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
