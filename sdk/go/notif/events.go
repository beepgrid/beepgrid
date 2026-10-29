package notif

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Event struct {
	Sequence       int64           `json:"sequence"`
	Kind           string          `json:"kind"`
	ActorID        string          `json:"actor_id"`
	ConversationID string          `json:"conversation_id"`
	RecipientID    string          `json:"recipient_id"`
	Data           json.RawMessage `json:"data"`
	CreatedAt      time.Time       `json:"created_at"`
}
type EventOptions struct {
	After          int64
	Summary        bool
	ConversationID string
}

var errEventFrame = errors.New("invalid Beep Grid event frame")

// StreamEvents reconnects from the last completely handled frame. Persist your
// cursor only after applying the event. A handler error stops without advancing.
// Cancellation only closes the local stream and never cancels durable requests.
func (c *Client) StreamEvents(ctx context.Context, options EventOptions, handle func(Event) error) error {
	if options.After < 0 || handle == nil {
		return fmt.Errorf("invalid event options")
	}
	base, err := url.Parse(c.BaseURL)
	if err != nil || base.Host == "" || base.User != nil || (base.Scheme != "http" && base.Scheme != "https") {
		return fmt.Errorf("invalid Beep Grid server URL")
	}
	cursor := options.After
	for ctx.Err() == nil {
		query := url.Values{"stream": {"1"}, "after": {strconv.FormatInt(cursor, 10)}}
		if options.Summary {
			query.Set("summary", "1")
		}
		if options.ConversationID != "" {
			query.Set("conversation_id", options.ConversationID)
		}
		req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(c.BaseURL, "/")+"/api/v1/events?"+query.Encode(), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("Last-Event-ID", strconv.FormatInt(cursor, 10))
		client := c.HTTP
		if client == nil {
			client = http.DefaultClient
		}
		res, err := client.Do(req)
		if err == nil {
			if res.StatusCode >= 400 {
				res.Body.Close()
				if res.StatusCode < 500 && res.StatusCode != 429 {
					return &APIError{Code: "EVENT_STREAM_REJECTED", Message: "Event stream rejected", Status: res.StatusCode}
				}
			} else if !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
				res.Body.Close()
				return fmt.Errorf("expected a Beep Grid event stream")
			} else {
				var handlerErr error
				err = consumeEvents(res.Body, &cursor, func(event Event) error { handlerErr = handle(event); return handlerErr })
				res.Body.Close()
				if handlerErr != nil {
					return handlerErr
				}
				if errors.Is(err, errEventFrame) {
					return err
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return ctx.Err()
}

func consumeEvents(reader io.Reader, cursor *int64, handle func(Event) error) error {
	const maxFrame = 8 << 20
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxFrame)
	var data []string
	id, kind := "", ""
	size := 0
	for scanner.Scan() {
		line := scanner.Text()
		size += len(line) + 1
		if size > maxFrame {
			return errEventFrame
		}
		if line == "" {
			if id != "" && len(data) > 0 {
				sequence, err := strconv.ParseInt(id, 10, 64)
				if err != nil || sequence < 0 {
					return errEventFrame
				}
				if sequence > *cursor {
					if kind != "cursor" {
						var event Event
						if json.Unmarshal([]byte(strings.Join(data, "\n")), &event) != nil || event.Sequence != sequence || event.Kind == "" {
							return errEventFrame
						}
						if err = handle(event); err != nil {
							return err
						}
					}
					*cursor = sequence
				}
			}
			data = nil
			id = ""
			kind = ""
			size = 0
		} else if !strings.HasPrefix(line, ":") {
			name, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")
			switch name {
			case "data":
				data = append(data, value)
			case "id":
				id = value
			case "event":
				kind = value
			}
		}
	}
	if scanner.Err() != nil {
		if errors.Is(scanner.Err(), bufio.ErrTooLong) {
			return errEventFrame
		}
		return scanner.Err()
	}
	return nil // An unterminated frame is discarded, never acknowledged.
}
