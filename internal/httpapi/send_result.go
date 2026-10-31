package httpapi

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"

	"github.com/beepgrid/beepgrid/internal/engine"
)

// Encode a large receipt one bounded message at a time. json.Encoder otherwise
// buffers the complete value, retaining a body-size * recipient-count buffer.
// The JSON bytes match the ordinary encoder, including escaping and field order.
func writeSendResult(w http.ResponseWriter, result engine.SendResult) error {
	buffer := bufio.NewWriterSize(w, 64*1024)
	if _, err := io.WriteString(buffer, `{"messages":[`); err != nil {
		return err
	}
	encoder := json.NewEncoder(jsonValueWriter{buffer})
	for i, message := range result.Messages {
		if i > 0 {
			if err := buffer.WriteByte(','); err != nil {
				return err
			}
		}
		if err := encoder.Encode(message); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(buffer, `],"conversation_id":`); err != nil {
		return err
	}
	if err := encoder.Encode(result.ConversationID); err != nil {
		return err
	}
	if _, err := io.WriteString(buffer, "}\n"); err != nil {
		return err
	}
	return buffer.Flush()
}

// Encode adds a newline to each value. Remove that delimiter so the containing
// receipt is byte-identical, while keeping the encoder's bounded reusable buffer.
type jsonValueWriter struct{ io.Writer }

func (w jsonValueWriter) Write(raw []byte) (int, error) {
	n := len(raw)
	if n > 0 && raw[n-1] == '\n' {
		raw = raw[:n-1]
	}
	written, err := w.Writer.Write(raw)
	if err != nil {
		return written, err
	}
	if written != len(raw) {
		return written, io.ErrShortWrite
	}
	return n, nil
}
