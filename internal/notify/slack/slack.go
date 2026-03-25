package slack

import (
	"context"
	"fmt"
	"time"

	"github.com/slack-go/slack"

	"github.com/atoolz/turnis/internal/notify"
)

type Sender struct {
	client *slack.Client
}

func New(client *slack.Client) *Sender {
	return &Sender{
		client: client,
	}
}

func (s *Sender) Name() notify.Channel {
	return notify.ChannelSlack
}

func (s *Sender) Send(ctx context.Context, msg notify.Message) (*notify.DeliveryResult, error) {
	emoji := severityEmoji(msg.Severity)

	blocks := []slack.Block{
		slack.NewHeaderBlock(
			slack.NewTextBlockObject(slack.PlainTextType, emoji+" "+msg.Title, true, false),
		),
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, msg.Body, false, false),
			nil, nil,
		),
		slack.NewContextBlock("",
			slack.NewTextBlockObject(slack.MarkdownType,
				fmt.Sprintf("Source: *%s* | %s", msg.Severity, time.Now().UTC().Format(time.RFC3339)),
				false, false,
			),
		),
		slack.NewActionBlock("alert_actions",
			slack.NewButtonBlockElement(
				fmt.Sprintf("ack_%s", msg.AlertID),
				"ack",
				slack.NewTextBlockObject(slack.PlainTextType, "Acknowledge", true, false),
			).WithStyle(slack.StylePrimary),
			slack.NewButtonBlockElement(
				fmt.Sprintf("resolve_%s", msg.AlertID),
				"resolve",
				slack.NewTextBlockObject(slack.PlainTextType, "Resolve", true, false),
			).WithStyle(slack.StyleDanger),
		),
	}

	opts := []slack.MsgOption{
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionText(fmt.Sprintf("%s %s", emoji, msg.Title), false),
	}

	channelID, ts, _, err := s.client.SendMessageContext(ctx, msg.Address, opts...)
	if err != nil {
		return nil, fmt.Errorf("sending slack message to %s: %w", msg.Address, err)
	}

	return &notify.DeliveryResult{
		MessageID:  ts,
		ChannelRef: channelID,
		Channel:    notify.ChannelSlack,
		Success:    true,
		SentAt:     time.Now(),
	}, nil
}

func severityEmoji(severity string) string {
	switch severity {
	case "critical":
		return "\U0001F6A8"
	case "warning":
		return "\u26A0\uFE0F"
	case "info":
		return "\u2139\uFE0F"
	default:
		return "\U0001F514"
	}
}
