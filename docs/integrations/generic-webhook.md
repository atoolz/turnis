# Generic Webhook Integration

This document covers the full ingest payload schema, deduplication behavior, and examples for sending alerts to Turnis via its webhook endpoint.

## Webhook URL

Every integration you create in Turnis gets a unique token. The webhook endpoint is:

```
POST https://turnis.yourcompany.com/webhook/<token>
```

Replace `<token>` with the token returned when you created the integration (see [Getting Started](../getting-started.md#7-create-a-webhook-integration)).

## Payload Schema

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "Turnis Incoming Alert",
  "type": "object",
  "required": ["title"],
  "properties": {
    "title": {
      "type": "string",
      "description": "Short summary of the alert. This is the only required field."
    },
    "message": {
      "type": "string",
      "description": "Longer description, runbook link, or any additional context."
    },
    "severity": {
      "type": "string",
      "enum": ["info", "warning", "critical"],
      "default": "info",
      "description": "Alert severity level."
    },
    "source": {
      "type": "string",
      "description": "Name of the system that generated the alert (e.g. 'prometheus', 'grafana', 'datadog')."
    },
    "fingerprint": {
      "type": "string",
      "description": "Stable identifier for deduplication. Alerts with the same fingerprint and integration are grouped."
    },
    "labels": {
      "type": "object",
      "additionalProperties": { "type": "string" },
      "description": "Arbitrary key-value pairs attached to the alert."
    }
  },
  "additionalProperties": true
}
```

### Field Summary

| Field         | Type              | Required | Default  | Description                                          |
|---------------|-------------------|----------|----------|------------------------------------------------------|
| `title`       | string            | Yes      |          | Short summary of the alert                           |
| `message`     | string            | No       | `""`     | Extended description or runbook link                 |
| `severity`    | string            | No       | `"info"` | One of `info`, `warning`, `critical`                 |
| `source`      | string            | No       | `""`     | Origin system name                                   |
| `fingerprint` | string            | No       | `""`     | Stable ID for deduplication (see below)              |
| `labels`      | map[string]string | No       | `{}`     | Arbitrary metadata key-value pairs                   |

## Deduplication via Fingerprint

When you include a `fingerprint` in the payload, Turnis checks whether any non-resolved alert with the same fingerprint already exists for that integration.

- If a match is found, Turnis returns the existing alert with `"deduplicated": true` and HTTP status `200 OK`. No new alert is created.
- If no match is found, Turnis creates a new alert and returns it with HTTP status `201 Created`.

This means you can safely call the webhook on every evaluation cycle of your monitoring tool without generating duplicate alerts. As long as the fingerprint stays the same, subsequent calls are no-ops.

Fingerprint values are scoped to the integration. Two different integrations can use the same fingerprint string without conflicting.

## Examples

### Fire an alert

```bash
curl -s -X POST "https://turnis.yourcompany.com/webhook/$WEBHOOK_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "High CPU on api-server-01",
    "message": "CPU usage exceeded 90% for 5 minutes. Runbook: https://wiki.example.com/runbooks/high-cpu",
    "severity": "critical",
    "source": "prometheus",
    "fingerprint": "cpu-high-api-server-01",
    "labels": {
      "env": "production",
      "service": "api",
      "host": "api-server-01"
    }
  }' | jq
```

Response (`201 Created`):

```json
{
  "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "integration_id": "int-abcdef12",
  "fingerprint": "cpu-high-api-server-01",
  "status": "firing",
  "title": "High CPU on api-server-01",
  "message": "CPU usage exceeded 90% for 5 minutes. Runbook: https://wiki.example.com/runbooks/high-cpu",
  "severity": "critical",
  "source": "prometheus",
  "created_at": "2026-03-25T14:30:00Z",
  "updated_at": "2026-03-25T14:30:00Z"
}
```

### Fire a duplicate (deduplication in action)

Sending the same fingerprint while the alert is still active:

```bash
curl -s -X POST "https://turnis.yourcompany.com/webhook/$WEBHOOK_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "High CPU on api-server-01",
    "severity": "critical",
    "fingerprint": "cpu-high-api-server-01"
  }' | jq
```

Response (`200 OK`):

```json
{
  "deduplicated": true,
  "alert": {
    "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "integration_id": "int-abcdef12",
    "fingerprint": "cpu-high-api-server-01",
    "status": "firing",
    "title": "High CPU on api-server-01",
    "severity": "critical",
    "source": "prometheus",
    "created_at": "2026-03-25T14:30:00Z",
    "updated_at": "2026-03-25T14:30:00Z"
  }
}
```

### Acknowledge an alert

Acknowledgment is done via the REST API, not the webhook endpoint:

```bash
curl -s -X POST "https://turnis.yourcompany.com/api/v1/alerts/$ALERT_ID/ack" \
  -H "Content-Type: application/json" \
  -d '{"user_id": "usr-alice-id"}' | jq
```

Response (`200 OK`):

```json
{
  "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "integration_id": "int-abcdef12",
  "fingerprint": "cpu-high-api-server-01",
  "status": "acknowledged",
  "title": "High CPU on api-server-01",
  "severity": "critical",
  "acknowledged_by": "usr-alice-id",
  "acknowledged_at": "2026-03-25T14:35:00Z",
  "created_at": "2026-03-25T14:30:00Z",
  "updated_at": "2026-03-25T14:35:00Z"
}
```

### Resolve an alert

```bash
curl -s -X POST "https://turnis.yourcompany.com/api/v1/alerts/$ALERT_ID/resolve" | jq
```

Response (`200 OK`):

```json
{
  "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "integration_id": "int-abcdef12",
  "fingerprint": "cpu-high-api-server-01",
  "status": "resolved",
  "title": "High CPU on api-server-01",
  "severity": "critical",
  "acknowledged_by": "usr-alice-id",
  "acknowledged_at": "2026-03-25T14:35:00Z",
  "resolved_at": "2026-03-25T14:40:00Z",
  "created_at": "2026-03-25T14:30:00Z",
  "updated_at": "2026-03-25T14:40:00Z"
}
```

### Minimal alert (title only)

```bash
curl -s -X POST "https://turnis.yourcompany.com/webhook/$WEBHOOK_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title": "Something broke"}' | jq
```

This creates a new alert every time it is called because there is no fingerprint for deduplication.

## Error Responses

All errors return a JSON object with an `error` field:

| HTTP Status | Meaning                                  | Example                                 |
|-------------|------------------------------------------|-----------------------------------------|
| `400`       | Invalid JSON or missing required fields  | `{"error": "title is required"}`        |
| `401`       | Missing or invalid webhook token         | `{"error": "invalid webhook token"}`    |
| `500`       | Internal server error                    | `{"error": "failed to create alert"}`   |

## Content-Type

The webhook endpoint expects `Content-Type: application/json`. Requests with other content types will return a `400 Bad Request`.
