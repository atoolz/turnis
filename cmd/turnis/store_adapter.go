package main

import (
	"context"

	"github.com/atoolz/turnis/internal/escalation"
	"github.com/atoolz/turnis/internal/store"
)

// storeAdapter adapts *store.DB to escalation.Store interface.
type storeAdapter struct {
	db *store.DB
}

func (a *storeAdapter) GetAlert(ctx context.Context, id string) (escalation.Alert, error) {
	al, err := a.db.GetAlert(ctx, id)
	if err != nil {
		return escalation.Alert{}, err
	}
	return escalation.Alert{
		ID:            al.ID,
		IntegrationID: al.IntegrationID,
		Title:         al.Title,
		Message:       al.Message,
		Severity:      string(al.Severity),
		Status:        string(al.Status),
	}, nil
}

func (a *storeAdapter) GetIntegration(ctx context.Context, id string) (escalation.Integration, error) {
	i, err := a.db.GetIntegration(ctx, id)
	if err != nil {
		return escalation.Integration{}, err
	}
	return escalation.Integration{
		ID:                 i.ID,
		EscalationPolicyID: i.EscalationPolicyID,
	}, nil
}

func (a *storeAdapter) GetPolicy(ctx context.Context, id string) (*escalation.Policy, error) {
	return a.db.GetPolicy(ctx, id)
}

func (a *storeAdapter) GetSchedule(ctx context.Context, id string) (escalation.Schedule, error) {
	s, err := a.db.GetSchedule(ctx, id)
	if err != nil {
		return escalation.Schedule{}, err
	}

	sched := escalation.Schedule{
		ID:             s.ID,
		Timezone:       s.Timezone,
		RotationType:   escalation.RotationType(s.RotationType),
		RotationLength: s.RotationLength,
		HandoffTime:    s.HandoffTime,
		HandoffDay:     s.HandoffDay,
		CreatedAt:      s.CreatedAt,
	}

	for _, l := range s.Layers {
		layer := escalation.Layer{Priority: l.Priority}
		for _, p := range l.Participants {
			layer.Participants = append(layer.Participants, escalation.Participant{UserID: p.UserID})
		}
		sched.Layers = append(sched.Layers, layer)
	}

	return sched, nil
}

func (a *storeAdapter) GetOverrides(ctx context.Context, scheduleID string) ([]escalation.Override, error) {
	ovs, err := a.db.GetOverrides(ctx, scheduleID)
	if err != nil {
		return nil, err
	}
	result := make([]escalation.Override, len(ovs))
	for i, o := range ovs {
		result[i] = escalation.Override{
			UserID:    o.UserID,
			StartTime: o.StartTime,
			EndTime:   o.EndTime,
		}
	}
	return result, nil
}

func (a *storeAdapter) GetUser(ctx context.Context, id string) (escalation.User, error) {
	u, err := a.db.GetUser(ctx, id)
	if err != nil {
		return escalation.User{}, err
	}
	return escalation.User{
		ID:        u.ID,
		Name:      u.Name,
		Email:     u.Email,
		Phone:     u.Phone,
		SlackID:   u.SlackID,
		NtfyTopic: u.NtfyTopic,
	}, nil
}

func (a *storeAdapter) GetNotificationRules(ctx context.Context, userID string) ([]escalation.NotificationRule, error) {
	rules, err := a.db.ListNotificationRules(ctx, userID)
	if err != nil {
		return nil, err
	}
	result := make([]escalation.NotificationRule, len(rules))
	for i, r := range rules {
		result[i] = escalation.NotificationRule{
			Channel:   r.Channel,
			Priority:  r.Priority,
			StartTime: r.StartTime,
			EndTime:   r.EndTime,
			Timezone:  r.Timezone,
		}
	}
	return result, nil
}

func (a *storeAdapter) RecordDelivery(ctx context.Context, alertID, userID, channel, address string, success bool, failureReason string) error {
	_, err := a.db.RecordDelivery(ctx, alertID, userID, channel, address, success, failureReason)
	return err
}

func (a *storeAdapter) MarkDeliveryEscalated(ctx context.Context, alertID string) error {
	return a.db.MarkDeliveryEscalated(ctx, alertID)
}
