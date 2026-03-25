# Getting Started

This guide walks you through running Turnis, creating your first on-call schedule, and firing a test alert.

## 1. Run Turnis

### Docker (recommended)

```bash
docker run -p 8080:8080 -v turnis-data:/data ghcr.io/atoolz/turnis:latest
```

### Binary

Download from [GitHub Releases](https://github.com/atoolz/turnis/releases), or:

```bash
# macOS
brew install atoolz/tap/turnis

# From source
go install github.com/atoolz/turnis/cmd/turnis@latest
```

Then run:

```bash
turnis serve
```

Turnis starts on port 8080 with SQLite by default. No config file needed for local testing.

## 2. Create a team

```bash
curl -s -X POST http://localhost:8080/api/v1/teams \
  -H "Content-Type: application/json" \
  -d '{"name": "backend", "slack_channel": "#backend-oncall"}' | jq
```

Save the `id` from the response. You'll need it for the next steps.

```bash
export TEAM_ID="<team-id-from-response>"
```

## 3. Create users

```bash
# Alice
curl -s -X POST http://localhost:8080/api/v1/users \
  -H "Content-Type: application/json" \
  -d "{\"name\": \"Alice\", \"email\": \"alice@example.com\", \"team_id\": \"$TEAM_ID\"}" | jq

# Bob
curl -s -X POST http://localhost:8080/api/v1/users \
  -H "Content-Type: application/json" \
  -d "{\"name\": \"Bob\", \"email\": \"bob@example.com\", \"team_id\": \"$TEAM_ID\"}" | jq
```

Save both user IDs:

```bash
export ALICE_ID="<alice-id>"
export BOB_ID="<bob-id>"
```

## 4. Create a schedule

Create a weekly rotation between Alice and Bob, with handoff every Monday at 09:00:

```bash
curl -s -X POST http://localhost:8080/api/v1/schedules \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"backend-primary\",
    \"team_id\": \"$TEAM_ID\",
    \"timezone\": \"UTC\",
    \"rotation_type\": \"weekly\",
    \"handoff_time\": \"09:00\",
    \"handoff_day\": \"monday\",
    \"layers\": [{
      \"priority\": 0,
      \"participants\": [
        {\"user_id\": \"$ALICE_ID\", \"position\": 0},
        {\"user_id\": \"$BOB_ID\", \"position\": 1}
      ]
    }]
  }" | jq
```

Save the schedule ID:

```bash
export SCHEDULE_ID="<schedule-id>"
```

## 5. Check who's on-call

```bash
curl -s "http://localhost:8080/api/v1/schedules/on-call?schedule_id=$SCHEDULE_ID" | jq
```

## 6. Create an escalation policy

Define what happens when an alert fires: notify the on-call person via webhook, wait 5 minutes, then escalate to Bob directly:

```bash
curl -s -X POST http://localhost:8080/api/v1/escalation-policies \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"backend-escalation\",
    \"team_id\": \"$TEAM_ID\",
    \"repeat\": 2,
    \"steps\": [
      {
        \"step_order\": 0,
        \"timeout_seconds\": 300,
        \"notify_schedule_id\": \"$SCHEDULE_ID\",
        \"notify_channel\": \"webhook\"
      },
      {
        \"step_order\": 1,
        \"timeout_seconds\": 300,
        \"notify_user_id\": \"$BOB_ID\",
        \"notify_channel\": \"webhook\"
      }
    ]
  }" | jq
```

Save the policy ID:

```bash
export POLICY_ID="<policy-id>"
```

## 7. Create a webhook integration

This gives you a URL that monitoring tools (Prometheus, Grafana, Datadog) can POST to:

```bash
curl -s -X POST http://localhost:8080/api/v1/integrations \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"prometheus\",
    \"team_id\": \"$TEAM_ID\",
    \"escalation_policy_id\": \"$POLICY_ID\"
  }" | jq
```

The response includes a `token`. Your webhook URL is:

```
POST http://localhost:8080/webhook/<token>
```

Save it:

```bash
export WEBHOOK_TOKEN="<token-from-response>"
```

## 8. Fire a test alert

```bash
curl -s -X POST "http://localhost:8080/webhook/$WEBHOOK_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "High CPU on api-server-01",
    "message": "CPU usage exceeded 90% for 5 minutes",
    "severity": "critical",
    "source": "prometheus",
    "fingerprint": "cpu-api-server-01"
  }' | jq
```

Check the server logs. You should see the escalation engine enqueue the alert and attempt to notify the on-call user.

## 9. Acknowledge the alert

```bash
export ALERT_ID="<alert-id-from-response>"

curl -s -X POST "http://localhost:8080/api/v1/alerts/$ALERT_ID/ack" \
  -H "Content-Type: application/json" \
  -d "{\"user_id\": \"$ALICE_ID\"}" | jq
```

The escalation timer is cancelled. No further notifications.

## 10. Resolve the alert

```bash
curl -s -X POST "http://localhost:8080/api/v1/alerts/$ALERT_ID/resolve" | jq
```

## Next steps

- Configure [Slack integration](../README.md) for interactive notifications (coming in v0.2.0)
- Set up [Twilio for SMS/voice](configuration.md#twilio) (coming in v0.3.0)
- Deploy to production with [Docker Compose](configuration.md#docker) or [Kubernetes](configuration.md#kubernetes)
- Read the [Configuration Reference](configuration.md) for all available options
