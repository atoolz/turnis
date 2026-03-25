package web

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/atoolz/turnis/internal/alert"
	"github.com/atoolz/turnis/internal/schedule"
	"github.com/atoolz/turnis/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

func NewHandler(db *store.DB) http.Handler {
	funcMap := template.FuncMap{
		"now":        func() time.Time { return time.Now() },
		"upper":      func(s string) string { return strings.ToUpper(s) },
		"formatTime": func(t time.Time) string { return t.Format("2006-01-02 15:04") },
		"truncate": func(s string, n int) string {
			if len(s) <= n {
				return s
			}
			return s[:n] + "..."
		},
	}

	tmpl := template.Must(
		template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html"),
	)

	r := chi.NewRouter()

	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(fmt.Sprintf("web: failed to sub static FS: %v", err))
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	r.Get("/", homeHandler(db, tmpl))
	r.Get("/web/oncall", onCallPartialHandler(db, tmpl))

	return r
}

type onCallEntry struct {
	ScheduleName string
	UserName     string
}

type severityCount struct {
	Critical int
	Warning  int
	Info     int
	Total    int
}

type activityEntry struct {
	Time    time.Time
	User    string
	Channel string
	Status  string
}

type homeData struct {
	Title    string
	Nav      string
	OnCall   []onCallEntry
	Alerts   severityCount
	Activity []activityEntry
}

func homeHandler(db *store.DB, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := buildHomeData(db, r)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
			slog.Error("failed to render home template", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	}
}

func onCallPartialHandler(db *store.DB, tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := buildHomeData(db, r)

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.ExecuteTemplate(w, "oncall_inner", data); err != nil {
			slog.Error("failed to render oncall partial", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	}
}

func buildHomeData(db *store.DB, r *http.Request) homeData {
	ctx := r.Context()
	data := homeData{
		Title: "Dashboard",
		Nav:   "home",
	}

	// On-call now
	schedules, err := db.ListSchedules(ctx)
	if err != nil {
		slog.Error("web: failed to list schedules", "error", err)
	}
	now := time.Now()
	for i := range schedules {
		s, err := db.GetSchedule(ctx, schedules[i].ID)
		if err != nil {
			slog.Error("web: failed to get schedule", "error", err, "id", schedules[i].ID)
			continue
		}
		overrides, err := db.GetOverrides(ctx, s.ID)
		if err != nil {
			slog.Error("web: failed to get overrides", "error", err, "id", s.ID)
		}
		userID := schedule.WhosOnCall(s, overrides, now)
		userName := userID
		if userID != "" {
			u, err := db.GetUser(ctx, userID)
			if err == nil {
				userName = u.Name
			}
		} else {
			userName = "Nobody"
		}
		data.OnCall = append(data.OnCall, onCallEntry{
			ScheduleName: s.Name,
			UserName:     userName,
		})
	}

	// Active alerts count by severity
	activeAlerts, err := db.ListAlerts(ctx, string(alert.StatusFiring), "")
	if err != nil {
		slog.Error("web: failed to list active alerts", "error", err)
	}
	ackedAlerts, err := db.ListAlerts(ctx, string(alert.StatusAcknowledged), "")
	if err != nil {
		slog.Error("web: failed to list acknowledged alerts", "error", err)
	}
	allActive := append(activeAlerts, ackedAlerts...)
	for _, a := range allActive {
		switch a.Severity {
		case alert.SeverityCritical:
			data.Alerts.Critical++
		case alert.SeverityWarning:
			data.Alerts.Warning++
		case alert.SeverityInfo:
			data.Alerts.Info++
		}
		data.Alerts.Total++
	}

	// Recent activity
	deliveries, err := db.ListRecentDeliveries(ctx, 5)
	if err != nil {
		slog.Error("web: failed to list recent deliveries", "error", err)
	}
	for _, d := range deliveries {
		status := "dispatched"
		if d.DeliveredAt != nil {
			status = "delivered"
		}
		if d.FailedAt != nil {
			status = "failed"
		}
		if d.AckedAt != nil {
			status = "acked"
		}
		data.Activity = append(data.Activity, activityEntry{
			Time:    d.DispatchedAt,
			User:    d.UserName,
			Channel: d.Channel,
			Status:  status,
		})
	}

	return data
}
