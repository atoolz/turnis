package api

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/atoolz/turnis/internal/escalation"
	"github.com/atoolz/turnis/internal/store"
)

type twimlResponse struct {
	XMLName xml.Name     `xml:"Response"`
	Say     *twimlSay    `xml:",omitempty"`
	Gather  *twimlGather `xml:",omitempty"`
}

type twimlSay struct {
	XMLName  xml.Name `xml:"Say"`
	Voice    string   `xml:"voice,attr,omitempty"`
	Language string   `xml:"language,attr,omitempty"`
	Text     string   `xml:",chardata"`
}

type twimlGather struct {
	XMLName   xml.Name  `xml:"Gather"`
	NumDigits int       `xml:"numDigits,attr"`
	Action    string    `xml:"action,attr"`
	Method    string    `xml:"method,attr"`
	Say       *twimlSay `xml:",omitempty"`
}

func twimlHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		alertID := chi.URLParam(r, "alertID")

		a, err := db.GetAlert(r.Context(), alertID)
		if err != nil {
			slog.Error("twiml: failed to get alert", "alert_id", alertID, "error", err)
			serveTwiMLError(w, "Alert not found.")
			return
		}

		gatherAction := fmt.Sprintf("/twiml/%s/gather", alertID)

		title := a.Title
		if len(title) > 500 {
			title = title[:497] + "..."
		}

		resp := twimlResponse{
			Gather: &twimlGather{
				NumDigits: 1,
				Action:    gatherAction,
				Method:    "POST",
				Say: &twimlSay{
					Voice:    "alice",
					Language: "en-US",
					Text:     fmt.Sprintf("Turnis alert: %s. Press 1 to acknowledge.", title),
				},
			},
		}

		writeTwiML(w, resp)
	}
}

func twimlGatherHandler(db *store.DB, engine *escalation.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		alertID := chi.URLParam(r, "alertID")

		if err := r.ParseForm(); err != nil {
			slog.Error("twiml gather: failed to parse form", "error", err)
			serveTwiMLError(w, "Invalid input.")
			return
		}

		digit := r.FormValue("Digits")

		if digit != "1" {
			resp := twimlResponse{
				Say: &twimlSay{
					Voice:    "alice",
					Language: "en-US",
					Text:     "No action taken. Goodbye.",
				},
			}
			writeTwiML(w, resp)
			return
		}

		callerID := r.FormValue("From")
		if callerID == "" {
			callerID = "voice-caller"
		}

		userID, err := resolveUserByPhone(r.Context(), db, callerID)
		if err != nil {
			slog.Warn("twiml gather: could not resolve user by phone, using phone as identifier", "phone", callerID, "error", err)
			userID = callerID
		}

		if _, err := db.AcknowledgeAlert(r.Context(), alertID, userID); err != nil {
			slog.Error("twiml gather: failed to acknowledge alert", "alert_id", alertID, "error", err)
			resp := twimlResponse{
				Say: &twimlSay{
					Voice:    "alice",
					Language: "en-US",
					Text:     "Could not acknowledge the alert. It may already be acknowledged or resolved.",
				},
			}
			writeTwiML(w, resp)
			return
		}

		if engine != nil {
			engine.Acknowledge(alertID)
		}

		resp := twimlResponse{
			Say: &twimlSay{
				Voice:    "alice",
				Language: "en-US",
				Text:     "Alert acknowledged. Thank you.",
			},
		}
		writeTwiML(w, resp)
	}
}

func writeTwiML(w http.ResponseWriter, resp twimlResponse) {
	w.Header().Set("Content-Type", "application/xml")
	// Let the first w.Write call implicitly commit the 200 status.
	fmt.Fprint(w, xml.Header)
	if err := xml.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("twiml: failed to encode xml", "error", err)
	}
}

func serveTwiMLError(w http.ResponseWriter, message string) {
	resp := twimlResponse{
		Say: &twimlSay{
			Voice:    "alice",
			Language: "en-US",
			Text:     message,
		},
	}
	writeTwiML(w, resp)
}

func resolveUserByPhone(ctx context.Context, db *store.DB, phone string) (string, error) {
	// Normalize to E.164: Twilio sends "+15551234567", users may be stored
	// with or without the "+" prefix.
	normalized := normalizePhone(phone)

	u, err := db.GetUserByPhone(ctx, normalized)
	if err != nil {
		// Try with "+" prefix if not already present
		if !strings.HasPrefix(normalized, "+") {
			u, err = db.GetUserByPhone(ctx, "+"+normalized)
			if err == nil {
				return u.ID, nil
			}
		}
		// Try without "+" prefix
		if strings.HasPrefix(normalized, "+") {
			u, err = db.GetUserByPhone(ctx, strings.TrimPrefix(normalized, "+"))
			if err == nil {
				return u.ID, nil
			}
		}
		return "", fmt.Errorf("no user found with phone %s: %w", phone, err)
	}
	return u.ID, nil
}

func normalizePhone(phone string) string {
	// Strip spaces, dashes, parentheses
	phone = strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' || r == '+' {
			return r
		}
		return -1
	}, phone)
	return phone
}
