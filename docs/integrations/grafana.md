# Grafana Integration

This guide configures Grafana Alerting to send alerts to Turnis via a webhook contact point.

## Prerequisites

- A running Turnis instance with a webhook integration created (see [Getting Started](../getting-started.md#7-create-a-webhook-integration))
- Your webhook token from the integration response
- Grafana 9.0+ with Grafana Alerting enabled (the default since Grafana 9)

## Step 1: Create a contact point

1. Open Grafana and navigate to **Alerting > Contact points**
2. Click **Add contact point**
3. Set the **Name** to `Turnis`
4. Under **Integration**, select **Webhook**
5. Set the **URL** to:
   ```
   https://turnis.yourcompany.com/webhook/YOUR_WEBHOOK_TOKEN
   ```
6. Set **HTTP Method** to `POST`
7. Leave **Authorization Header** empty (the token in the URL is the authentication)
8. Expand **Optional Webhook settings**:
   - Set **Max Alerts** to `0` (no limit)
   - Leave **Username** and **Password** empty
9. Click **Test** to send a test notification, then **Save contact point**

## Step 2: Configure the message template

Grafana's default webhook payload does not match the Turnis schema. You need a custom message template that transforms the Grafana payload into Turnis format.

1. Navigate to **Alerting > Contact points > Message templates**
2. Click **Add message template**
3. Set the **Name** to `turnis`
4. Paste this template:

```go
{{ define "turnis" }}
{
  "title": "{{ (index .Alerts 0).Labels.alertname }}",
  "message": "{{ (index .Alerts 0).Annotations.description }}",
  "severity": "{{ if eq (index .Alerts 0).Labels.severity "critical" }}critical{{ else if eq (index .Alerts 0).Labels.severity "warning" }}warning{{ else }}info{{ end }}",
  "source": "grafana",
  "fingerprint": "{{ (index .Alerts 0).Fingerprint }}",
  "labels": {
    {{ range $i, $k := (index .Alerts 0).Labels.SortedPairs }}{{ if $i }},{{ end }}"{{ $k.Name }}": "{{ $k.Value }}"{{ end }}
  }
}
{{ end }}
```

5. Click **Save**

6. Go back to your `Turnis` contact point and set the **Message** field to use the template:
   - In **Optional Webhook settings**, find the **Body** field
   - Set it to: `{{ template "turnis" . }}`

Alternatively, if your Grafana version supports the **Custom body** field directly in the webhook contact point settings, paste the template content there.

## Step 3: Set up a notification policy

1. Navigate to **Alerting > Notification policies**
2. Either edit the default policy or create a new one:
   - Click **New nested policy** (or **Edit** on the default)
   - Under **Contact point**, select `Turnis`
   - Optionally add matchers to route only specific alerts (e.g., `severity = critical`)
3. Click **Save policy**

## What Grafana sends (default payload)

Without a custom template, Grafana sends a payload like this to webhook contact points:

```json
{
  "receiver": "turnis",
  "status": "firing",
  "alerts": [
    {
      "status": "firing",
      "labels": {
        "alertname": "HighMemoryUsage",
        "severity": "critical",
        "instance": "web-01",
        "grafana_folder": "Infrastructure"
      },
      "annotations": {
        "description": "Memory usage on web-01 is above 95% for 10 minutes.",
        "summary": "High memory on web-01",
        "runbook_url": "https://wiki.example.com/runbooks/high-memory"
      },
      "startsAt": "2026-03-25T14:00:00Z",
      "endsAt": "0001-01-01T00:00:00Z",
      "generatorURL": "https://grafana.example.com/alerting/grafana/abc123/view",
      "fingerprint": "a1b2c3d4e5f6",
      "silenceURL": "https://grafana.example.com/alerting/silence/new?...",
      "dashboardURL": "https://grafana.example.com/d/abc123",
      "panelURL": "https://grafana.example.com/d/abc123?viewPanel=1",
      "values": {
        "B": 97.3
      }
    }
  ],
  "groupLabels": {
    "alertname": "HighMemoryUsage"
  },
  "commonLabels": {
    "alertname": "HighMemoryUsage",
    "severity": "critical"
  },
  "commonAnnotations": {
    "description": "Memory usage on web-01 is above 95% for 10 minutes."
  },
  "externalURL": "https://grafana.example.com",
  "version": "1",
  "groupKey": "{}:{alertname=\"HighMemoryUsage\"}",
  "truncatedAlerts": 0,
  "title": "[FIRING:1] HighMemoryUsage critical",
  "state": "alerting",
  "message": "**Firing**\n\nValue: B=97.3\nLabels:\n - alertname = HighMemoryUsage\n..."
}
```

## What Turnis receives (after template transformation)

After applying the `turnis` message template, the payload sent to Turnis looks like:

```json
{
  "title": "HighMemoryUsage",
  "message": "Memory usage on web-01 is above 95% for 10 minutes.",
  "severity": "critical",
  "source": "grafana",
  "fingerprint": "a1b2c3d4e5f6",
  "labels": {
    "alertname": "HighMemoryUsage",
    "grafana_folder": "Infrastructure",
    "instance": "web-01",
    "severity": "critical"
  }
}
```

## Alternative: Grafana with an HTTP adapter

If you prefer not to use Grafana message templates, you can use the same adapter pattern described in the [Prometheus Alertmanager guide](prometheus-alertmanager.md). The Grafana webhook payload is very similar to Alertmanager's payload because Grafana Alerting is built on the same alert model.

## Field mapping reference

| Grafana field                  | Turnis field    | Notes                                                  |
|--------------------------------|-----------------|--------------------------------------------------------|
| `alerts[].labels.alertname`    | `title`         | Alert rule name becomes the title                      |
| `alerts[].annotations.description` | `message`   | Description annotation becomes the message             |
| `alerts[].labels.severity`     | `severity`      | Mapped to `info`, `warning`, or `critical`             |
| `alerts[].fingerprint`        | `fingerprint`   | Grafana's auto-generated fingerprint, used as-is       |
| All `alerts[].labels`          | `labels`        | Passed through as key-value pairs                      |
| (hardcoded)                    | `source`        | Set to `"grafana"` by the template                     |

## Verifying the integration

1. In Grafana, go to your `Turnis` contact point and click **Test**
2. Check Turnis for the test alert:

```bash
curl -s http://localhost:8080/api/v1/alerts | jq
```

3. To test with a real alert, create a simple alert rule:
   - Navigate to **Alerting > Alert rules > New alert rule**
   - Create a rule that evaluates to `true` (e.g., a static threshold below a known value)
   - Assign it to the notification policy that routes to `Turnis`
   - Wait for the evaluation interval and check Turnis for the alert

## Grafana OnCall migration note

If you are migrating from Grafana OnCall (archived in 2024), the main changes are:

- Replace the OnCall integration URL with the Turnis webhook URL
- OnCall's auto-acknowledge and auto-resolve features are handled by Turnis via fingerprint-based deduplication and the REST API ack/resolve endpoints
- Escalation policies are configured in Turnis, not in Grafana
- On-call schedules live in Turnis, not in the Grafana OnCall plugin
