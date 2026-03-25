# Turnis

## What is Turnis

Open-source, Slack-native on-call management tool. Single Go binary. Replaces the archived Grafana OnCall OSS and provides a migration path for Opsgenie refugees (sunsetting April 2027).

Philosophy: **"On-call, not on-everything."**

Turnis does schedules, escalation, and notifications. Nothing else. No monitoring, no status pages, no postmortems, no AI.

## Architecture

### Core Principles
- Single binary, zero external dependencies for basic use (SQLite embedded)
- Slack is the primary interface, web UI is secondary
- Every notification is internally a webhook dispatch
- BYOT (Bring Your Own Twilio) for SMS/voice
- ntfy for push notifications (free, no mobile app to build)

### Tech Stack
- **Language:** Go 1.23+
- **Database:** SQLite (default, via modernc.org/sqlite) / PostgreSQL (production)
- **Queue:** In-process goroutine pool (no Redis/RabbitMQ for MVP)
- **Frontend:** Server-rendered HTML + htmx (embedded in binary via embed.FS)
- **API:** REST with OpenAPI spec
- **Config:** YAML file + environment variables

### Project Structure
```
turnis/
├── cmd/turnis/           # CLI entrypoint (cobra)
├── internal/
│   ├── schedule/         # On-call schedule CRUD, rotation logic
│   ├── escalation/       # Escalation policy engine
│   ├── alert/            # Alert ingestion, grouping, routing
│   ├── notify/           # Notification dispatch (webhook-first)
│   │   ├── slack/        # Slack interactive messages + DMs
│   │   ├── twilio/       # SMS + voice via user's Twilio account
│   │   ├── ntfy/         # Push notifications via ntfy.sh
│   │   ├── webhook/      # Generic webhook dispatch
│   │   └── email/        # SMTP email
│   ├── api/              # REST API handlers
│   ├── store/            # Database layer (SQLite/Postgres)
│   ├── config/           # Configuration loading
│   └── web/              # Web UI (htmx templates)
├── migrations/           # SQL migration files
├── configs/              # Example config files
├── scripts/              # Build, release, migration scripts
└── .github/workflows/    # CI/CD
```

### Notification Architecture (Layered)
```
Layer 0: Webhook-first core (all notifications are webhook dispatches internally)
Layer 1: Built-in free channels (Slack, Email, Discord)
Layer 2: BYOT channels (Twilio SMS/Voice via user's own account)
Layer 3: Push via ntfy (free, self-hostable, no mobile app needed)
Layer 4: Generic webhook escape hatch (any HTTP endpoint)
```

### Data Model (Core Entities)
- **Team**: group of users
- **Schedule**: who is on-call when (weekly/daily/custom rotation)
- **Override**: temporary schedule swap
- **EscalationPolicy**: multi-step chain with timeouts
- **Integration**: webhook endpoint that receives alerts
- **Alert**: incoming incident from monitoring tools
- **AlertGroup**: grouped alerts by fingerprint
- **NotificationTarget**: channel + address + ack callback
- **DeliveryAttempt**: audit log of every notification sent

## Development

### Running locally
```bash
go run ./cmd/turnis serve --config configs/turnis.example.yaml
```

### Database
SQLite by default. File created at `./turnis.db`. For Postgres, set `database.driver: postgres` in config.

### Key competitors to understand
- GoAlert (github.com/target/goalert) - 2.7k stars, web-UI-first, Go
- Grafana OnCall (archived) - Python/Django, was the best OSS option
- PagerDuty - $21-49/user/month, the standard

### Differentiators to maintain
1. Slack-NATIVE (not Slack-compatible). Primary interface is Slack.
2. Single binary. No Redis, no Celery, no separate workers.
3. BYOT Twilio. User's own account, ~$0.79/month instead of $49/user/month.
4. ntfy for push. No mobile app to build/maintain.
5. Deliberately limited scope. Schedules + escalation + notifications only.

## Conventions
- Go standard project layout
- Error handling: wrap errors with context using fmt.Errorf
- Logging: slog (stdlib structured logging)
- Testing: stdlib testing + testify for assertions
- Database: raw SQL with parameterized queries (no ORM)
- API: chi router + OpenAPI spec
- Config: viper for YAML + env var loading
