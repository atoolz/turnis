package twilio

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/atoolz/turnis/internal/notify"
)

type SMSSender struct {
	accountSID string
	authToken  string
	fromNumber string
	client     *http.Client
}

func NewSMS(accountSID, authToken, fromNumber string) *SMSSender {
	return &SMSSender{
		accountSID: accountSID,
		authToken:  authToken,
		fromNumber: fromNumber,
		client:     &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *SMSSender) Name() notify.Channel {
	return notify.ChannelSMS
}

func (s *SMSSender) Send(ctx context.Context, msg notify.Message) (*notify.DeliveryResult, error) {
	if msg.Address == "" {
		return nil, fmt.Errorf("no phone number for SMS delivery")
	}

	body := fmt.Sprintf("[%s] %s: %s", strings.ToUpper(msg.Severity), msg.Title, msg.Body)
	if msg.AckURL != "" {
		body += fmt.Sprintf("\nAck: %s", msg.AckURL)
	}

	if len(body) > 1500 {
		body = body[:1497] + "..."
	}

	endpoint := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", s.accountSID)

	form := url.Values{}
	form.Set("To", msg.Address)
	form.Set("From", s.fromNumber)
	form.Set("Body", body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating twilio SMS request: %w", err)
	}

	req.SetBasicAuth(s.accountSID, s.authToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending twilio SMS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("twilio SMS returned status %d: %s", resp.StatusCode, string(errBody))
	}
	_, _ = io.Copy(io.Discard, resp.Body)

	return &notify.DeliveryResult{
		Channel: notify.ChannelSMS,
		Success: true,
		SentAt:  time.Now(),
	}, nil
}
