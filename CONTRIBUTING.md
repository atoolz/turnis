# Contributing to Turnis

We actively seek contributors and co-maintainers.

## Getting started

```bash
git clone https://github.com/atoolz/turnis.git
cd turnis
go build ./cmd/turnis
go test -race ./...
```

Requires Go 1.23+. No other dependencies needed for local development.

## Development workflow

1. Pick an issue from the [roadmap](https://github.com/atoolz/turnis/issues/2)
2. Comment on the issue to claim it
3. Create a branch: `git checkout -b feat/your-feature`
4. Make your changes
5. Run tests: `go test -race ./...`
6. Run vet: `go vet ./...`
7. Commit with a descriptive message
8. Open a PR against `main`

## Code conventions

- **Error handling:** wrap errors with context using `fmt.Errorf("doing X: %w", err)`
- **Logging:** use `slog` (stdlib structured logging), never `fmt.Println`
- **Database:** raw SQL with parameterized queries, no ORM. New store methods go in separate files per entity.
- **API:** chi router, JSON request/response, `writeJSON`/`writeError` helpers
- **Config:** viper for YAML + env vars, all keys documented in `docs/configuration.md`
- **Testing:** stdlib `testing` + `testify/assert`. Table-driven tests preferred.

## Project structure

```
cmd/turnis/           # CLI entrypoint and server wiring
internal/
  alert/              # Alert types and deduplication
  api/                # REST API handlers (one file per entity)
  config/             # Configuration loading
  escalation/         # Escalation engine and policy logic
  notify/             # Notification dispatcher and senders
  schedule/           # On-call rotation logic
  store/              # Database layer (one file per entity)
```

## What we need help with

- **Slack integration** (v0.2.0): the main differentiator, needs someone who has built Slack bots
- **Twilio/email senders** (v0.3.0): implementing the `notify.Sender` interface
- **Web UI** (v0.4.0): htmx + Go templates, no React
- **Migration tooling** (v0.5.0): parsing Grafana OnCall and Opsgenie export formats

## Becoming a co-maintainer

If you submit 2-3 quality PRs and show sustained interest, we'll offer write access. The goal is bus factor > 1 from day one.
