package api

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	AlertsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "turnis_alerts_total",
			Help: "Total number of alerts ingested.",
		},
		[]string{"status", "severity"},
	)

	NotificationsDispatchedTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "turnis_notifications_dispatched_total",
			Help: "Total number of notifications dispatched.",
		},
		[]string{"channel", "success"},
	)

	EscalationStepsFiredTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "turnis_escalation_steps_fired_total",
			Help: "Total number of escalation steps fired.",
		},
	)

	ActiveAlerts = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "turnis_active_alerts",
			Help: "Number of currently active (non-resolved) alerts.",
		},
	)

	NotificationLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "turnis_notification_latency_seconds",
			Help:    "Latency of notification dispatch in seconds.",
			Buckets: prometheus.DefBuckets,
		},
	)
)
