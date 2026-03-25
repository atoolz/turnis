# Configuration Reference

Turnis is configured via a YAML file and/or environment variables. Every config key can be overridden with a `TURNIS_` prefixed environment variable.

## Config file location

By default, Turnis looks for `turnis.yaml` in the current directory. Override with:

```bash
turnis serve --config /etc/turnis/turnis.yaml
```

## Environment variable mapping

YAML keys map to env vars by uppercasing and replacing dots with underscores:

| YAML key | Environment variable |
|---|---|
| `server.port` | `TURNIS_SERVER_PORT` |
| `server.host` | `TURNIS_SERVER_HOST` |
| `server.base_url` | `TURNIS_SERVER_BASE_URL` |
| `database.driver` | `TURNIS_DATABASE_DRIVER` |
| `database.dsn` | `TURNIS_DATABASE_DSN` |
| `slack.bot_token` | `TURNIS_SLACK_BOT_TOKEN` |
| `slack.app_token` | `TURNIS_SLACK_APP_TOKEN` |
| `slack.signing_secret` | `TURNIS_SLACK_SIGNING_SECRET` |
| `twilio.account_sid` | `TURNIS_TWILIO_ACCOUNT_SID` |
| `twilio.auth_token` | `TURNIS_TWILIO_AUTH_TOKEN` |
| `twilio.from_number` | `TURNIS_TWILIO_FROM_NUMBER` |
| `ntfy.server` | `TURNIS_NTFY_SERVER` |
| `email.smtp_host` | `TURNIS_EMAIL_SMTP_HOST` |
| `email.smtp_port` | `TURNIS_EMAIL_SMTP_PORT` |
| `email.from` | `TURNIS_EMAIL_FROM` |
| `email.username` | `TURNIS_EMAIL_USERNAME` |
| `email.password` | `TURNIS_EMAIL_PASSWORD` |
| `log.level` | `TURNIS_LOG_LEVEL` |

Environment variables take precedence over the config file.

## Full reference

```yaml
# Server settings
server:
  host: 0.0.0.0          # Listen address (default: 0.0.0.0)
  port: 8080              # Listen port (default: 8080)
  base_url: http://localhost:8080  # Public URL for ack/resolve links in notifications

# Database
database:
  driver: sqlite          # "sqlite" or "postgres" (default: sqlite)
  dsn: turnis.db          # SQLite file path, or Postgres connection string
  # Postgres example:
  # driver: postgres
  # dsn: postgres://user:pass@localhost:5432/turnis?sslmode=disable

# Slack (coming in v0.2.0)
slack:
  bot_token: ""           # xoxb-... Bot User OAuth Token
  app_token: ""           # xapp-... App-Level Token (for Socket Mode)
  signing_secret: ""      # Signing secret for verifying Slack requests

# Twilio - Bring Your Own Account (coming in v0.3.0)
# SMS costs ~$0.0079/message. A team paging 100 times/month pays ~$0.79.
twilio:
  account_sid: ""         # Twilio Account SID
  auth_token: ""          # Twilio Auth Token
  from_number: ""         # Twilio phone number (+1234567890)

# ntfy push notifications (free, no mobile app needed)
# Install the ntfy app on your phone and subscribe to your personal topic.
ntfy:
  server: https://ntfy.sh  # ntfy server URL (default: https://ntfy.sh)
  # Self-hosted: server: https://ntfy.yourcompany.com

# Email (SMTP)
email:
  smtp_host: ""           # SMTP server hostname
  smtp_port: 587          # SMTP port (587 for STARTTLS, 465 for SSL)
  from: ""                # From address
  username: ""            # SMTP username
  password: ""            # SMTP password

# Logging
log:
  level: info             # debug, info, warn, error (default: info)
```

## Database

### SQLite (default)

No setup needed. Turnis creates a `turnis.db` file in the current directory. WAL mode is enabled automatically for better concurrent read performance.

```yaml
database:
  driver: sqlite
  dsn: turnis.db
```

For Docker, mount a volume:

```bash
docker run -v turnis-data:/data -e TURNIS_DATABASE_DSN=/data/turnis.db ghcr.io/atoolz/turnis
```

### PostgreSQL

For production deployments with multiple replicas or high write throughput:

```yaml
database:
  driver: postgres
  dsn: postgres://turnis:secret@db.example.com:5432/turnis?sslmode=require
```

Create the database first:

```sql
CREATE DATABASE turnis;
CREATE USER turnis WITH PASSWORD 'secret';
GRANT ALL PRIVILEGES ON DATABASE turnis TO turnis;
```

Turnis runs migrations automatically on startup.

## Docker

### Try it out (ephemeral, data lost on restart)

```bash
docker run -p 8080:8080 ghcr.io/atoolz/turnis:latest
```

### With persistent storage (recommended)

```bash
docker run -p 8080:8080 \
  -v turnis-data:/data \
  -e TURNIS_DATABASE_DSN=/data/turnis.db \
  ghcr.io/atoolz/turnis:latest
```

### With environment config

```bash
docker run -p 8080:8080 \
  -v turnis-data:/data \
  -e TURNIS_DATABASE_DSN=/data/turnis.db \
  -e TURNIS_SERVER_BASE_URL=https://turnis.yourcompany.com \
  -e TURNIS_NTFY_SERVER=https://ntfy.yourcompany.com \
  -e TURNIS_LOG_LEVEL=debug \
  ghcr.io/atoolz/turnis:latest
```

### Docker Compose

```yaml
services:
  turnis:
    image: ghcr.io/atoolz/turnis:latest
    ports:
      - "8080:8080"
    volumes:
      - turnis-data:/data
    environment:
      TURNIS_DATABASE_DSN: /data/turnis.db
      TURNIS_SERVER_BASE_URL: https://turnis.yourcompany.com
    restart: unless-stopped

volumes:
  turnis-data:
```

## Kubernetes

See [Helm chart documentation](https://github.com/atoolz/turnis/tree/main/charts/turnis) (coming in v0.5.0).

## Notification channels

### Webhook (built-in, always available)

Every integration gets a webhook URL automatically. Point your monitoring tool at:

```
POST https://turnis.yourcompany.com/webhook/<token>
```

Payload:

```json
{
  "title": "Alert title",
  "message": "Alert description",
  "severity": "critical",
  "source": "prometheus",
  "fingerprint": "unique-alert-id"
}
```

The `fingerprint` field enables deduplication: alerts with the same fingerprint and integration are grouped instead of creating duplicates.

### ntfy (built-in, free)

[ntfy](https://ntfy.sh) provides push notifications without building a mobile app.

1. Install the ntfy app on your phone ([Android](https://play.google.com/store/apps/details?id=io.heckel.ntfy), [iOS](https://apps.apple.com/app/ntfy/id1625396347))
2. Subscribe to a unique topic (e.g., `turnis-alice-oncall`)
3. Set the `ntfy_topic` field on your user: `PUT /api/v1/users/{id}` with `{"ntfy_topic": "turnis-alice-oncall"}`

Critical alerts are sent with `priority: urgent`, which bypasses Do Not Disturb on most phones.

For self-hosted ntfy:

```yaml
ntfy:
  server: https://ntfy.yourcompany.com
```
