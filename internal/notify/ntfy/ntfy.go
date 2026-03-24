package ntfy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/ahlfrm/turnis/internal/notify"
)

type Sender struct {
	server string
	client *http.Client
}

func New(server string) *Sender {
	if server == "" {
		server = "https://ntfy.sh"
	}
	return &Sender{
		server: server,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Sender) Name() notify.Channel {
	return notify.ChannelPush
}

func (s *Sender) Send(ctx context.Context, msg notify.Message) (*notify.DeliveryResult, error) {
	endpoint := fmt.Sprintf("%s/%s", s.server, url.PathEscape(msg.Address))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(msg.Body))
	if err != nil {
		return nil, fmt.Errorf("creating ntfy request: %w", err)
	}

	req.Header.Set("Title", msg.Title)
	req.Header.Set("Tags", "rotating_light")

	if msg.Urgent {
		req.Header.Set("Priority", "urgent")
	} else {
		req.Header.Set("Priority", "high")
	}

	if msg.AckURL != "" {
		req.Header.Set("Actions", fmt.Sprintf("http, Acknowledge, %s, method=POST", msg.AckURL))
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending ntfy notification: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ntfy returned status %d", resp.StatusCode)
	}

	return &notify.DeliveryResult{
		Channel: notify.ChannelPush,
		Success: true,
		SentAt:  time.Now(),
	}, nil
}
