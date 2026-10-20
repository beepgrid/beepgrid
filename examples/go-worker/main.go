package main

import (
	"context"
	"fmt"
	"github.com/notif-dev/notif/sdk/go/notif"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	launcher := notif.New(os.Getenv("NOTIF_URL"), os.Getenv("NOTIF_TOKEN"))
	c, session, e := launcher.Enroll(ctx, notif.EnrollInput{Agent: "examples/go", Name: "Go worker", Capabilities: []string{"message"}})
	if e != nil {
		panic(e)
	}
	fmt.Println("Agent:", session.Agent.ID)
	e = c.Run(ctx, func(ctx context.Context, d notif.Delivery) error {
		if d.Payload.Type == "message" && d.Payload.Message != nil {
			_, e := c.Send(ctx, notif.SendInput{ConversationID: d.Payload.Message.ConversationID, Body: "Go worker received: " + d.Payload.Message.Body}, "reply/"+d.ID)
			return e
		}
		return nil
	})
	if e != nil && ctx.Err() == nil {
		panic(e)
	}
}
