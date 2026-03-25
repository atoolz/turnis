# Turnis

**On-call, not on-everything.**

Open-source, Slack-native on-call management. Single Go binary. No Redis, no Celery, no Python runtime.

## Why Turnis exists

**Grafana OnCall OSS was archived.** It was the best open-source on-call tool, with 28 million Docker pulls. It died because the free version was cannibalizing Grafana Cloud IRM (their paid product), and Grafana is preparing for IPO. A business decision, not a technical one.

**Opsgenie is sunsetting in April 2027.** Atlassian acquired it for $295M and is now killing it. Thousands of teams need to migrate with no good path forward.

**PagerDuty charges $21-49/user/month.** For a 20-person team, that's $5k-12k/year just to know who answers the phone when the system goes down.

The abandoned segment: teams of 5-50 developers who self-hosted Grafana OnCall, European teams with data sovereignty requirements, and teams that simply refuse to pay PagerDuty prices for what should be a simple problem.

## What Turnis does

When an alert fires (from Prometheus, Grafana, Datadog, or anything that sends a webhook), Turnis:

1. Identifies who is on-call right now
2. Sends a Slack message with Ack/Resolve buttons
3. If no response in N minutes, escalates to the next person
4. If needed, sends SMS/voice call via Twilio (your own account)
5. Push notifications via ntfy (free, no custom mobile app required)

That's it. No monitoring, no status pages, no postmortems, no AI.

## How it's different

| | Turnis | PagerDuty | Grafana OnCall | GoAlert |
|---|---|---|---|---|
| **Cost** | Free (OSS) | $21-49/user/mo | Archived | Free (OSS) |
| **Primary interface** | Slack | Web dashboard | Grafana plugin | Web dashboard |
| **Deployment** | Single binary | SaaS | 3+ containers + Redis + Grafana | Single binary |
| **SMS/Voice** | BYOT Twilio (~$0.79/mo) | Included (at $49/user) | Required Grafana Cloud | BYOT Twilio |
| **Push notifications** | ntfy (free) | Proprietary app | Required Grafana Cloud | Not available |
| **Setup time** | 60 seconds | Account signup | Hours of configuration | Minutes |

**Slack-native** means Slack is the primary interface, not just a notification channel. Create schedules, swap shifts, acknowledge alerts, check who's on-call, all from Slack.

**BYOT (Bring Your Own Twilio)** means you plug in your own Twilio account for SMS and voice. A team paging 100 times per month pays ~$0.79 in Twilio costs, not $49/user/month.

**Single binary** means `docker run turnis` and it works. SQLite embedded, no external dependencies. Postgres optional for production.

## Why the successor won't die the same way

Grafana killed OnCall OSS because it was too good for free and undermined their own Cloud revenue. Turnis has no Cloud to protect. The conflict that killed OnCall is the advantage of being independent.

## Quick start

```bash
docker run -p 8080:8080 -v turnis-data:/data ghcr.io/atoolz/turnis:latest
```

Or download the binary:

```bash
curl -fsSL https://turnis.dev/install.sh | sh
turnis serve
```

## Configuration

```yaml
server:
  port: 8080
  base_url: https://turnis.yourcompany.com

database:
  driver: sqlite  # or postgres
  dsn: turnis.db

slack:
  bot_token: xoxb-your-bot-token
  signing_secret: your-signing-secret

twilio:
  account_sid: your-account-sid
  auth_token: your-auth-token
  from_number: +1234567890

ntfy:
  server: https://ntfy.sh  # or self-hosted
```

All settings can be overridden via environment variables with the `TURNIS_` prefix (e.g., `TURNIS_SLACK_BOT_TOKEN`).

## API

```bash
# Create a team
curl -X POST http://localhost:8080/api/v1/teams \
  -H "Content-Type: application/json" \
  -d '{"name": "backend", "slack_channel": "#backend-oncall"}'

# Create a schedule
curl -X POST http://localhost:8080/api/v1/schedules \
  -H "Content-Type: application/json" \
  -d '{"name": "backend-primary", "team_id": "...", "rotation_type": "weekly"}'

# Check who's on-call
curl http://localhost:8080/api/v1/schedules/on-call?team_id=...

# Create a webhook integration
curl -X POST http://localhost:8080/api/v1/integrations \
  -H "Content-Type: application/json" \
  -d '{"name": "prometheus", "team_id": "...", "escalation_policy_id": "..."}'
# Returns a webhook URL: POST /webhook/{token}
```

## Competitor landscape

| Tool | Stars | Status | Why not enough |
|---|---|---|---|
| Grafana OnCall | 3.9k | Archived (March 2026) | Dead. Python/Django, required Grafana + Redis + Celery. |
| GoAlert | 2.7k | Active | Web-UI-first, not Slack-native. No ntfy support. |
| OneUptime | 6.7k | Active | Kitchen sink (monitoring + status pages + on-call). Does nothing well. |
| Keep | 11.5k | Active | Alert aggregation only. No on-call scheduling. |
| PagerDuty | N/A | Active (SaaS) | $21-49/user/month. |
| Opsgenie | N/A | Sunsetting April 2027 | Atlassian is killing it. |

## Monetization

**Free forever (AGPLv3):** Schedules, escalation policies, Slack bot, BYOT Twilio, ntfy push, webhooks, full REST API.

**Turnis Cloud (paid, coming later):** Managed hosting, SSO/SAML, audit log retention, SLA guarantee, phone tree escalation, branded mobile push. $5-12/user/month.

## License

Turnis is licensed under the [GNU Affero General Public License v3.0](LICENSE) (AGPLv3).

This means:
- You can use, modify, and distribute Turnis freely
- If you modify Turnis and offer it as a network service, you must release your modifications under AGPLv3
- You can use Turnis commercially (run it for your company, charge customers who use your service)
- You cannot take Turnis, make it proprietary, and sell it as a competing SaaS without releasing the source

For organizations that need a different license (e.g., embedding Turnis in proprietary software), contact us for a commercial license.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines. We actively seek co-maintainers.
