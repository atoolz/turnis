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

type VoiceSender struct {
	accountSID string
	authToken  string
	fromNumber string
	baseURL    string
	client     *http.Client
}

func NewVoice(accountSID, authToken, fromNumber, baseURL string) *VoiceSender {
	return &VoiceSender{
		accountSID: accountSID,
		authToken:  authToken,
		fromNumber: fromNumber,
		baseURL:    strings.TrimRight(baseURL, "/"),
		client:     &http.Client{Timeout: 15 * time.Second},
	}
}

func (s *VoiceSender) Name() notify.Channel {
	return notify.ChannelVoice
}

func (s *VoiceSender) Send(ctx context.Context, msg notify.Message) (*notify.DeliveryResult, error) {
	if msg.Address == "" {
		return nil, fmt.Errorf("no phone number for voice delivery")
	}

	twimlURL := fmt.Sprintf("%s/twiml/%s", s.baseURL, url.PathEscape(msg.AlertID))

	endpoint := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Calls.json", s.accountSID)

	form := url.Values{}
	form.Set("To", msg.Address)
	form.Set("From", s.fromNumber)
	form.Set("Url", twimlURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating twilio voice request: %w", err)
	}

	req.SetBasicAuth(s.accountSID, s.authToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("initiating twilio voice call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("twilio voice returned status %d: %s", resp.StatusCode, string(body))
	}
	_, _ = io.Copy(io.Discard, resp.Body)

	return &notify.DeliveryResult{
		Channel: notify.ChannelVoice,
		Success: true,
		SentAt:  time.Now(),
	}, nil
}
