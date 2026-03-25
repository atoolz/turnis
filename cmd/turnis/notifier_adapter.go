package main

import (
	"context"

	"github.com/atoolz/turnis/internal/escalation"
	"github.com/atoolz/turnis/internal/notify"
)

// dispatcherAdapter adapts notify.Dispatcher to escalation.Notifier interface.
type dispatcherAdapter struct {
	dispatcher *notify.Dispatcher
}

func (a *dispatcherAdapter) Notify(ctx context.Context, channel, address string, al escalation.Alert, user escalation.User, ackURL, resolveURL string) (bool, error) {
	msg := notify.Message{
		AlertID:    al.ID,
		UserID:     user.ID,
		Channel:    notify.Channel(channel),
		Address:    address,
		Title:      al.Title,
		Body:       al.Message,
		Severity:   al.Severity,
		AckURL:     ackURL,
		ResolveURL: resolveURL,
		Urgent:     al.Severity == "critical",
	}

	result, err := a.dispatcher.Dispatch(ctx, msg)
	if err != nil {
		return false, err
	}

	return result.Success, nil
}
