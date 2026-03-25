package email

import (
	"context"
	"crypto/tls"
	"fmt"
	"html"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/atoolz/turnis/internal/notify"
)

type Sender struct {
	host     string
	port     int
	from     string
	username string
	password string
}

func New(host string, port int, from, username, password string) *Sender {
	if port == 0 {
		port = 587
	}
	return &Sender{
		host:     host,
		port:     port,
		from:     from,
		username: username,
		password: password,
	}
}

func (s *Sender) Name() notify.Channel {
	return notify.ChannelEmail
}

func (s *Sender) Send(ctx context.Context, msg notify.Message) (*notify.DeliveryResult, error) {
	if msg.Address == "" {
		return nil, fmt.Errorf("no email address for delivery")
	}

	subject := fmt.Sprintf("[Turnis] [%s] %s", strings.ToUpper(msg.Severity), msg.Title)
	body := buildHTMLBody(msg)

	emailMsg := buildMIME(s.from, msg.Address, subject, body)

	addr := net.JoinHostPort(s.host, fmt.Sprintf("%d", s.port))
	tlsConfig := &tls.Config{ServerName: s.host}

	var conn net.Conn
	if s.port == 465 {
		// Port 465: implicit TLS (SMTPS), connect with TLS from the start.
		var err error
		conn, err = tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", addr, tlsConfig)
		if err != nil {
			return nil, fmt.Errorf("TLS connecting to SMTP server %s: %w", addr, err)
		}
	} else {
		var err error
		conn, err = net.DialTimeout("tcp", addr, 10*time.Second)
		if err != nil {
			return nil, fmt.Errorf("connecting to SMTP server %s: %w", addr, err)
		}
	}

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("creating SMTP client: %w", err)
	}
	defer client.Close()

	// Attempt STARTTLS if the server advertises it, regardless of port.
	tlsActive := s.port == 465
	if !tlsActive {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return nil, fmt.Errorf("STARTTLS failed: %w", err)
			}
			tlsActive = true
		}
	}

	if s.username != "" {
		if !tlsActive {
			return nil, fmt.Errorf("SMTP server does not support TLS, refusing to send credentials")
		}
		auth := smtp.PlainAuth("", s.username, s.password, s.host)
		if err := client.Auth(auth); err != nil {
			return nil, fmt.Errorf("SMTP auth failed: %w", err)
		}
	}

	if err := client.Mail(s.from); err != nil {
		return nil, fmt.Errorf("SMTP MAIL FROM: %w", err)
	}
	if err := client.Rcpt(msg.Address); err != nil {
		return nil, fmt.Errorf("SMTP RCPT TO: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return nil, fmt.Errorf("SMTP DATA: %w", err)
	}
	if _, err := w.Write([]byte(emailMsg)); err != nil {
		return nil, fmt.Errorf("writing email body: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("closing email data: %w", err)
	}

	client.Quit()

	return &notify.DeliveryResult{
		Channel: notify.ChannelEmail,
		Success: true,
		SentAt:  time.Now(),
	}, nil
}

func buildMIME(from, to, subject, htmlBody string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("From: %s\r\n", from))
	sb.WriteString(fmt.Sprintf("To: %s\r\n", to))
	sb.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(htmlBody)
	return sb.String()
}

func buildHTMLBody(msg notify.Message) string {
	severityColor := "#f0ad4e"
	switch msg.Severity {
	case "critical":
		severityColor = "#d9534f"
	case "info":
		severityColor = "#5bc0de"
	}

	// Escape all user-controlled content to prevent XSS.
	title := html.EscapeString(msg.Title)
	body := html.EscapeString(msg.Body)
	severity := html.EscapeString(strings.ToUpper(msg.Severity))

	var buttons string
	if msg.AckURL != "" && strings.HasPrefix(msg.AckURL, "http") {
		buttons += fmt.Sprintf(`<a href="%s" style="display:inline-block;padding:10px 20px;background:#5cb85c;color:#fff;text-decoration:none;border-radius:4px;margin-right:8px;">Acknowledge</a>`, html.EscapeString(msg.AckURL))
	}
	if msg.ResolveURL != "" && strings.HasPrefix(msg.ResolveURL, "http") {
		buttons += fmt.Sprintf(`<a href="%s" style="display:inline-block;padding:10px 20px;background:#d9534f;color:#fff;text-decoration:none;border-radius:4px;">Resolve</a>`, html.EscapeString(msg.ResolveURL))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="font-family:sans-serif;padding:20px;">
  <div style="border-left:4px solid %s;padding:12px 16px;margin-bottom:16px;">
    <h2 style="margin:0 0 8px 0;">%s</h2>
    <p style="margin:0 0 8px 0;color:#666;">Severity: <strong>%s</strong></p>
    <p style="margin:0;">%s</p>
  </div>
  <div style="margin-top:16px;">%s</div>
</body>
</html>`, severityColor, title, severity, body, buttons)
}
