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

//go:embed templates/*.html templates/partials/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

var funcMap = template.FuncMap{
	"now":        func() time.Time { return time.Now() },
	"upper":      func(s string) string { return strings.ToUpper(s) },
	"formatTime": func(t time.Time) string { return t.Format("2006-01-02 15:04") },
	"eq":         func(a, b string) bool { return a == b },
	"truncate": func(s string, n int) string {
		if len(s) <= n {
			return s
		}
		return s[:n] + "..."
	},
	"safeClass": func(s string) string {
		allowed := map[string]bool{
			"firing": true, "acknowledged": true, "resolved": true,
			"critical": true, "warning": true, "info": true, "error": true,
			"dispatched": true, "delivered": true, "failed": true, "acked": true,
		}
		if allowed[strings.ToLower(s)] {
			return strings.ToLower(s)
		}
		return "unknown"
	},
}

func pageTemplate(page string) *template.Template {
	return template.Must(template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/layout.html", "templates/"+page))
}

func pageWithPartials(page string, partials ...string) *template.Template {
	files := []string{"templates/layout.html", "templates/" + page}
	for _, p := range partials {
		files = append(files, "templates/partials/"+p)
	}
	return template.Must(template.New("").Funcs(funcMap).ParseFS(templateFS, files...))
}

func partialTmpl(files ...string) *template.Template {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = "templates/partials/" + f
	}
	return template.Must(template.New("").Funcs(funcMap).ParseFS(templateFS, paths...))
}

// NewHandler creates the web UI handler.
// TODO(#26): Add CSRF protection when API auth lands.
func NewHandler(db *store.DB) http.Handler {
	home := pageTemplate("home.html")
	scheds := pageTemplate("schedules.html")
	sched := pageTemplate("schedule.html")
	alts := pageWithPartials("alerts.html", "alert_list.html")
	tms := pageTemplate("teams.html")
	tm := pageTemplate("team.html")
	usrs := pageTemplate("users.html")
	oncallP := partialTmpl("oncall.html")
	alertP := partialTmpl("alert_list.html")
	editP := partialTmpl("user_edit.html")

	r := chi.NewRouter()
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))
	r.Get("/", pg(db, home, buildHome))
	r.Get("/web/oncall", partial(db, oncallP, "oncall_inner", buildHome))
	r.Get("/schedules", pg(db, scheds, buildSchedules))
	r.Get("/schedules/{id}", schedDetail(db, sched))
	r.Post("/schedules/{id}/overrides", overrideWeb(db))
	r.Get("/alerts", pg(db, alts, buildAlerts))
	r.Get("/web/partials/alert-list", partial(db, alertP, "alert_table_inner", buildAlerts))
	r.Get("/teams", pg(db, tms, buildTeams))
	r.Get("/teams/{id}", teamDetail(db, tm))
	r.Post("/teams", teamCreate(db))
	r.Get("/users", pg(db, usrs, buildUsers))
	r.Post("/users", userCreate(db))
	r.Put("/users/{id}", userUpdate(db))
	r.Get("/users/{id}/edit", userEdit(db, editP))
	return r
}

type builder func(*store.DB, *http.Request) any

func pg(db *store.DB, t *template.Template, b builder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.ExecuteTemplate(w, "layout.html", b(db, r)); err != nil {
			slog.Error("render error", "error", err)
		}
	}
}

func partial(db *store.DB, t *template.Template, block string, b builder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.ExecuteTemplate(w, block, b(db, r)); err != nil {
			slog.Error("render error", "error", err)
		}
	}
}

// --- Data ---

type onCallEntry struct{ ScheduleID, ScheduleName, UserName string }
type sevCount struct{ Critical, Warning, Info, Total int }
type actEntry struct{ Time time.Time; User, Channel, Status string }
type schedRow struct{ ID, Name, HandoffDesc, OnCallUser, NextHandoff string }
type layerV struct{ Priority int; Participants []partV }
type partV struct{ Position int; UserName string }
type overV struct{ UserName string; StartTime, EndTime time.Time; Reason string }
type alertR struct{ ID, Status, Severity, Title, Source string; CreatedAt time.Time }
type userR struct{ ID, Name, Email, Phone, TeamID, TeamName, SlackID, NtfyTopic string }

// --- Home ---

func buildHome(db *store.DB, r *http.Request) any {
	ctx := r.Context()
	type D struct{ Title, Nav string; OnCall []onCallEntry; Alerts sevCount; Activity []actEntry }
	d := D{Title: "Dashboard", Nav: "home"}
	ss, _ := db.ListSchedules(ctx)
	now := time.Now()
	for i := range ss {
		s, err := db.GetSchedule(ctx, ss[i].ID)
		if err != nil { continue }
		ov, _ := db.GetOverrides(ctx, s.ID)
		uid := schedule.WhosOnCall(s, ov, now)
		un := "Nobody"
		if uid != "" { if u, e := db.GetUser(ctx, uid); e == nil { un = u.Name } }
		d.OnCall = append(d.OnCall, onCallEntry{s.ID, s.Name, un})
	}
	fi, _ := db.ListAlerts(ctx, string(alert.StatusFiring), "")
	ak, _ := db.ListAlerts(ctx, string(alert.StatusAcknowledged), "")
	for _, a := range append(fi, ak...) {
		switch a.Severity {
		case alert.SeverityCritical: d.Alerts.Critical++
		case alert.SeverityWarning: d.Alerts.Warning++
		case alert.SeverityInfo: d.Alerts.Info++
		}
		d.Alerts.Total++
	}
	dl, _ := db.ListRecentDeliveries(ctx, 5)
	for _, x := range dl {
		st := "dispatched"
		if x.DeliveredAt != nil { st = "delivered" }
		if x.FailedAt != nil { st = "failed" }
		if x.AckedAt != nil { st = "acked" }
		d.Activity = append(d.Activity, actEntry{x.DispatchedAt, x.UserName, x.Channel, st})
	}
	return d
}

// --- Schedules ---

func buildSchedules(db *store.DB, r *http.Request) any {
	ctx := r.Context()
	type D struct{ Title, Nav string; Schedules []schedRow }
	d := D{Title: "Schedules", Nav: "schedules"}
	ss, _ := db.ListSchedules(ctx)
	now := time.Now()
	for i := range ss {
		s, err := db.GetSchedule(ctx, ss[i].ID)
		if err != nil { continue }
		ov, _ := db.GetOverrides(ctx, s.ID)
		uid := schedule.WhosOnCall(s, ov, now)
		un := "Nobody"
		if uid != "" { if u, e := db.GetUser(ctx, uid); e == nil { un = u.Name } }
		d.Schedules = append(d.Schedules, schedRow{s.ID, s.Name,
			fmt.Sprintf("%s, %s %s", s.RotationType, s.HandoffDay, s.HandoffTime),
			un, nextHandoff(s, now)})
	}
	return d
}

func schedDetail(db *store.DB, t *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		s, err := db.GetSchedule(ctx, chi.URLParam(r, "id"))
		if err != nil { http.Error(w, "Not found", 404); return }
		ov, _ := db.GetOverrides(ctx, s.ID)
		uid := schedule.WhosOnCall(s, ov, time.Now())
		un := "Nobody"
		if uid != "" { if u, e := db.GetUser(ctx, uid); e == nil { un = u.Name } }
		var ls []layerV
		for _, l := range s.Layers {
			lv := layerV{Priority: l.Priority}
			for _, p := range l.Participants {
				nm := p.UserID
				if u, e := db.GetUser(ctx, p.UserID); e == nil { nm = u.Name }
				lv.Participants = append(lv.Participants, partV{p.Position, nm})
			}
			ls = append(ls, lv)
		}
		var os []overV
		for _, o := range ov {
			nm := o.UserID
			if u, e := db.GetUser(ctx, o.UserID); e == nil { nm = u.Name }
			os = append(os, overV{nm, o.StartTime, o.EndTime, o.Reason})
		}
		us, _ := db.ListUsers(ctx, "")
		type D struct{ Title, Nav, OnCallUser string; Schedule schedule.Schedule; Layers []layerV; Overrides []overV; Users []store.User }
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.ExecuteTemplate(w, "layout.html", D{s.Name, "schedules", un, *s, ls, os, us}); err != nil {
			slog.Error("render error", "error", err)
		}
	}
}

func overrideWeb(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		r.ParseForm()
		et, err := time.Parse("2006-01-02T15:04", r.FormValue("end_time"))
		if err != nil { http.Error(w, "Invalid end time", 400); return }
		st := time.Now().UTC()
		if v := r.FormValue("start_time"); v != "" {
			if t, e := time.Parse("2006-01-02T15:04", v); e == nil { st = t }
		}
		if _, err := db.CreateOverride(r.Context(), id, r.FormValue("user_id"), st, et, r.FormValue("reason")); err != nil {
			http.Error(w, "Failed", 500); return
		}
		http.Redirect(w, r, "/schedules/"+id, http.StatusSeeOther)
	}
}

// --- Alerts ---

func buildAlerts(db *store.DB, r *http.Request) any {
	ctx := r.Context()
	st := r.URL.Query().Get("status")
	sv := r.URL.Query().Get("severity")
	aa, _ := db.ListAlerts(ctx, st, "")
	var rows []alertR
	for _, a := range aa {
		if sv != "" && string(a.Severity) != sv { continue }
		rows = append(rows, alertR{a.ID, string(a.Status), string(a.Severity), a.Title, a.Source, a.CreatedAt})
	}
	type D struct{ Title, Nav, FilterStatus, FilterSeverity string; Alerts []alertR }
	return D{"Alerts", "alerts", st, sv, rows}
}

// --- Teams ---

func buildTeams(db *store.DB, r *http.Request) any {
	ts, _ := db.ListTeams(r.Context())
	type D struct{ Title, Nav string; Teams []store.Team }
	return D{"Teams", "teams", ts}
}

func teamDetail(db *store.DB, t *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tm, err := db.GetTeam(ctx, chi.URLParam(r, "id"))
		if err != nil { http.Error(w, "Not found", 404); return }
		ms, _ := db.ListUsers(ctx, tm.ID)
		is, _ := db.GetIntegrationsByTeam(ctx, tm.ID)
		type D struct{ Title, Nav string; Team store.Team; Members []store.User; Integrations []store.Integration }
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := t.ExecuteTemplate(w, "layout.html", D{tm.Name, "teams", *tm, ms, is}); err != nil {
			slog.Error("render error", "error", err)
		}
	}
}

func teamCreate(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		n := r.FormValue("name")
		if n == "" { http.Error(w, "Name required", 400); return }
		db.CreateTeam(r.Context(), n, r.FormValue("slack_channel"))
		http.Redirect(w, r, "/teams", http.StatusSeeOther)
	}
}

// --- Users ---

func buildUsers(db *store.DB, r *http.Request) any {
	ctx := r.Context()
	us, _ := db.ListUsers(ctx, "")
	ts, _ := db.ListTeams(ctx)
	tm := make(map[string]string)
	for _, t := range ts { tm[t.ID] = t.Name }
	var rows []userR
	for _, u := range us {
		rows = append(rows, userR{u.ID, u.Name, u.Email, u.Phone, u.TeamID, tm[u.TeamID], u.SlackID, u.NtfyTopic})
	}
	type D struct{ Title, Nav string; Users []userR; Teams []store.Team }
	return D{"Users", "users", rows, ts}
}

func userCreate(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.FormValue("name") == "" || r.FormValue("email") == "" { http.Error(w, "Name and email required", 400); return }
		db.CreateUser(r.Context(), r.FormValue("name"), r.FormValue("email"), r.FormValue("phone"), r.FormValue("slack_id"), r.FormValue("ntfy_topic"), r.FormValue("team_id"))
		http.Redirect(w, r, "/users", http.StatusSeeOther)
	}
}

func userUpdate(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		db.UpdateUser(r.Context(), chi.URLParam(r, "id"), r.FormValue("name"), r.FormValue("email"), r.FormValue("phone"), r.FormValue("slack_id"), r.FormValue("ntfy_topic"), r.FormValue("team_id"))
		http.Redirect(w, r, "/users", http.StatusSeeOther)
	}
}

func userEdit(db *store.DB, t *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		u, err := db.GetUser(ctx, chi.URLParam(r, "id"))
		if err != nil { http.Error(w, "Not found", 404); return }
		ts, _ := db.ListTeams(ctx)
		type D struct{ User store.User; Teams []store.Team }
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		t.ExecuteTemplate(w, "user_edit_row", D{*u, ts})
	}
}

// --- Helpers ---

func nextHandoff(s *schedule.Schedule, now time.Time) string {
	loc, _ := time.LoadLocation(s.Timezone)
	if loc == nil { loc = time.UTC }
	ln := now.In(loc)
	ps := strings.SplitN(s.HandoffTime, ":", 2)
	h, m := 9, 0
	if len(ps) == 2 { fmt.Sscanf(ps[0], "%d", &h); fmt.Sscanf(ps[1], "%d", &m) }
	switch s.RotationType {
	case schedule.RotationDaily:
		n := time.Date(ln.Year(), ln.Month(), ln.Day(), h, m, 0, 0, loc)
		l := s.RotationLength; if l <= 0 { l = 1 }
		if !n.After(ln) { n = n.AddDate(0, 0, l) }
		return n.Format("Mon Jan 2 15:04")
	case schedule.RotationWeekly:
		td := pwday(s.HandoffDay)
		n := time.Date(ln.Year(), ln.Month(), ln.Day(), h, m, 0, 0, loc)
		for n.Weekday() != td || !n.After(ln) { n = n.AddDate(0, 0, 1) }
		return n.Format("Mon Jan 2 15:04")
	}
	return "Unknown"
}

func pwday(s string) time.Weekday {
	switch strings.ToLower(s) {
	case "sunday": return time.Sunday
	case "tuesday": return time.Tuesday
	case "wednesday": return time.Wednesday
	case "thursday": return time.Thursday
	case "friday": return time.Friday
	case "saturday": return time.Saturday
	default: return time.Monday
	}
}
