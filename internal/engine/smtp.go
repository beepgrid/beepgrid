package engine

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"time"
)

// smtpFailure never stores the remote response or addresses, which can contain
// credentials or message content. A negative SMTP reply is definite rejection;
// a lost final DATA reply is ambiguous and must not be retried automatically.
func smtpFailure(stage string, err error, final bool) DispatchResult {
	r := DispatchResult{State: "failed", Retry: !final, Detail: "SMTP " + stage + ": " + safeError(err)}
	var response *textproto.Error
	if errors.As(err, &response) && response.Code >= 400 && response.Code < 600 {
		r.Retry = response.Code < 500
		r.Detail = fmt.Sprintf("SMTP %s rejected with code %d", stage, response.Code)
	} else if final {
		r.State = "unknown"
	}
	return r
}

func (s *Store) sendMail(ctx context.Context, c Connection, to, body string) DispatchResult {
	bad := DispatchResult{State: "failed", Detail: "Invalid SMTP configuration or address."}
	addr := c.Config["smtp_addr"]
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return bad
	}
	from, err := mail.ParseAddress(c.Config["from"])
	if err != nil {
		return bad
	}
	recipient, err := mail.ParseAddress(to)
	if err != nil {
		return bad
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	conn, err := s.dial(ctx, "tcp", addr)
	if err != nil {
		return smtpFailure("connect", err, false)
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	// net/smtp has no context API. Closing the connection also interrupts a
	// blocked greeting, TLS handshake, body write or final response on shutdown.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return smtpFailure("greeting", err, false)
	}
	defer client.Close()
	if yes, _ := client.Extension("STARTTLS"); yes {
		if err = client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return smtpFailure("TLS", err, false)
		}
	} else if !s.AllowLocal {
		return DispatchResult{State: "failed", Detail: "SMTP TLS required."}
	}
	if c.Config["username"] != "" {
		if err = client.Auth(smtp.PlainAuth("", c.Config["username"], c.Secret, host)); err != nil {
			return smtpFailure("authentication", err, false)
		}
	}
	if err = client.Mail(from.Address); err != nil {
		return smtpFailure("MAIL", err, false)
	}
	if err = client.Rcpt(recipient.Address); err != nil {
		return smtpFailure("RCPT", err, false)
	}
	w, err := client.Data()
	if err != nil {
		return smtpFailure("DATA command", err, false)
	}
	if _, err = fmt.Fprintf(w, "From: %s\r\nTo: %s\r\nSubject: Notif update\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", from.String(), recipient.String(), body); err != nil {
		// The DATA terminator has not been sent, so this transaction cannot
		// have been accepted. DotWriter escapes terminators within the body.
		return smtpFailure("body", err, false)
	}
	if err = w.Close(); err != nil {
		return smtpFailure("DATA result", err, true)
	}
	// The final 250 is authoritative. A lost QUIT response cannot undo it or
	// occupy a delivery slot for the full send timeout.
	_ = conn.SetDeadline(minTime(deadline, time.Now().Add(200*time.Millisecond)))
	_ = client.Quit()
	return DispatchResult{State: "provider_accepted", Detail: "SMTP server accepted the message."}
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
