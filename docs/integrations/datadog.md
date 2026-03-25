# Datadog Integration

This guide configures Datadog to send monitor alerts to Turnis via Datadog's webhook integration.

## Prerequisites

- A running Turnis instance with a webhook integration created (see [Getting Started](../getting-started.md#7-create-a-webhook-integration))
- Your webhook token from the integration response
- A Datadog account with permissions to configure integrations

## Step 1: Create a webhook integration in Datadog

1. In Datadog, navigate to **Integrations > Integrations** and search for **Webhooks**
2. Click **Configure** (or **Install** if not yet installed)
3. Scroll down to **Webhooks** and click **New**
4. Fill in the fields:
   - **Name**: `turnis`
   - **URL**: `https://turnis.yourcompany.com/webhook/YOUR_WEBHOOK_TOKEN`
   - **Payload**: paste the JSON template below
   - **Custom Headers**: add `Content-Type: application/json`
5. Check **Encode as form** is **unchecked** (the payload must be sent as raw JSON)
6. Click **Save**

## Step 2: Configure the payload template

Paste this payload template in the **Payload** field:

```json
{
  "title": "$ALERT_TITLE",
  "message": "$EVENT_MSG",
  "severity": "warning",
  "source": "datadog",
  "fingerprint": "dd-$ALERT_ID",
  "labels": {
    "alert_id": "$ALERT_ID",
    "alert_type": "$ALERT_TYPE",
    "alert_status": "$ALERT_STATUS",
    "hostname": "$HOSTNAME",
    "org_name": "$ORG_NAME",
    "event_type": "$EVENT_TYPE",
    "alert_query": "$ALERT_QUERY",
    "alert_scope": "$ALERT_SCOPE",
    "last_updated": "$LAST_UPDATED",
    "link": "$LINK"
  }
}
```

### Datadog variable to Turnis field mapping

| Datadog variable     | Turnis field            | Description                                             |
|----------------------|-------------------------|---------------------------------------------------------|
| `$ALERT_TITLE`       | `title`                 | Monitor name and triggering scope                       |
| `$EVENT_MSG`         | `message`               | Monitor message body (supports markdown from Datadog)   |
| `$ALERT_PRIORITY`    | `severity`              | Maps to Turnis severity (see mapping table below)       |
| `$ALERT_ID`          | `fingerprint` (prefix)  | Unique monitor ID, prefixed with `dd-` for namespacing  |
| `$ALERT_TYPE`        | `labels.alert_type`     | `error`, `warning`, `info`, or `success`                |
| `$ALERT_STATUS`      | `labels.alert_status`   | `Triggered`, `Recovered`, `No Data`, etc.               |
| `$HOSTNAME`          | `labels.hostname`       | Host that triggered the alert                           |
| `$ORG_NAME`          | `labels.org_name`       | Your Datadog organization name                          |
| `$EVENT_TYPE`        | `labels.event_type`     | Event type string                                       |
| `$ALERT_QUERY`       | `labels.alert_query`    | The monitor query that triggered                        |
| `$ALERT_SCOPE`       | `labels.alert_scope`    | Scope tags (e.g., `host:web-01,env:prod`)               |
| `$LAST_UPDATED`      | `labels.last_updated`   | Timestamp of the last status change                     |
| `$LINK`              | `labels.link`           | Direct link to the Datadog monitor                      |

### Severity mapping

Datadog's `$ALERT_PRIORITY` returns a value like `P1`, `P2`, `P3`, `P4`, or `normal`. Turnis accepts `critical`, `warning`, and `info`. To handle this, use a conditional payload or accept the Datadog value as-is in the labels and set a fixed severity.

**Option A: Fixed severity with priority in labels (simplest)**

```json
{
  "title": "$ALERT_TITLE",
  "message": "$EVENT_MSG",
  "severity": "critical",
  "source": "datadog",
  "fingerprint": "dd-$ALERT_ID",
  "labels": {
    "priority": "$ALERT_PRIORITY",
    "alert_type": "$ALERT_TYPE",
    "hostname": "$HOSTNAME",
    "link": "$LINK"
  }
}
```

**Option B: Map severity via Datadog conditional variables**

Datadog supports conditional formatting in webhook payloads. Use `$ALERT_TYPE` which returns `error`, `warning`, `info`, or `success`:

```json
{
  "title": "$ALERT_TITLE",
  "message": "$EVENT_MSG",
  "severity": "$ALERT_TYPE",
  "source": "datadog",
  "fingerprint": "dd-$ALERT_ID",
  "labels": {
    "hostname": "$HOSTNAME",
    "alert_scope": "$ALERT_SCOPE",
    "link": "$LINK"
  }
}
```

Turnis treats unrecognized severity values as `info`, so `$ALERT_TYPE` values of `error` and `success` both default to `info`. For the most accurate mapping, use Option A and set the severity explicitly based on the monitor's importance.

**Option C: Use different webhooks per severity**

Create multiple webhook integrations in both Datadog and Turnis:

- `turnis-critical` for P1/P2 monitors
- `turnis-warning` for P3 monitors
- `turnis-info` for P4+ monitors

Each points to a different Turnis integration with appropriate escalation policies.

## Step 3: Attach the webhook to monitors

For each Datadog monitor that should page via Turnis:

1. Open the monitor and click **Edit**
2. In the **Notify your team** section, add `@webhook-turnis` in the message body
3. Click **Save**

Example monitor message:

```
CPU usage on {{host.name}} exceeded threshold.

Current value: {{value}}
Threshold: {{threshold}}

Runbook: https://wiki.example.com/runbooks/high-cpu

@webhook-turnis
```

To apply to all monitors of a certain type, use Datadog's **Manage Monitors** page to bulk-edit the notification recipients.

## Example: what Datadog sends to Turnis

After variable substitution, Turnis receives a payload like this:

```json
{
  "title": "[Triggered] High CPU on web-01",
  "message": "CPU usage on web-01 exceeded threshold.\n\nCurrent value: 94.5%\nThreshold: 90%\n\nRunbook: https://wiki.example.com/runbooks/high-cpu",
  "severity": "critical",
  "source": "datadog",
  "fingerprint": "dd-12345678",
  "labels": {
    "alert_id": "12345678",
    "alert_type": "error",
    "alert_status": "Triggered",
    "hostname": "web-01",
    "org_name": "MyCompany",
    "event_type": "metric_alert_monitor",
    "alert_query": "avg(last_5m):avg:system.cpu.user{host:web-01} > 90",
    "alert_scope": "host:web-01",
    "last_updated": "1711375200",
    "link": "https://app.datadoghq.com/monitors/12345678"
  }
}
```

## Handling resolved alerts

Datadog sends the webhook again when a monitor recovers, with `$ALERT_STATUS` set to `Recovered` and `$ALERT_TYPE` set to `success`. Because the fingerprint (`dd-$ALERT_ID`) remains the same, Turnis will deduplicate the recovery notification against the active alert. The original alert stays in its current state.

To resolve alerts in Turnis when Datadog recovers them, you have two options:

1. **Manual resolution**: Resolve via Turnis Slack, web UI, or REST API
2. **Automated resolution**: Deploy a small adapter that calls the Turnis resolve endpoint when it receives a Datadog recovery webhook. See the adapter pattern in the [Prometheus Alertmanager guide](prometheus-alertmanager.md).

## Verifying the integration

1. In Datadog, go to **Integrations > Webhooks** and find your `turnis` webhook
2. Click **Test** to send a test payload
3. Check Turnis for the test alert:

```bash
curl -s https://turnis.yourcompany.com/api/v1/alerts | jq
```

4. To test with a real monitor, create a monitor with a low threshold that will trigger immediately, add `@webhook-turnis` to its message, and wait for it to fire.

## All available Datadog variables

For reference, these are all the variables you can use in the webhook payload template:

| Variable           | Description                                      |
|--------------------|--------------------------------------------------|
| `$ALERT_ID`        | Unique ID of the monitor                         |
| `$ALERT_TITLE`     | Monitor title with triggering context             |
| `$ALERT_TYPE`      | `error`, `warning`, `info`, or `success`         |
| `$ALERT_STATUS`    | `Triggered`, `Recovered`, `No Data`, etc.        |
| `$ALERT_QUERY`     | The monitor query                                |
| `$ALERT_SCOPE`     | Comma-separated scope tags                       |
| `$ALERT_PRIORITY`  | Priority (`P1`, `P2`, `P3`, `P4`, `normal`)     |
| `$EVENT_MSG`       | Monitor message body                             |
| `$EVENT_TYPE`      | Event type identifier                            |
| `$HOSTNAME`        | Host that triggered the alert                    |
| `$ORG_NAME`        | Datadog organization name                        |
| `$LAST_UPDATED`    | Unix timestamp of last status change             |
| `$LINK`            | URL to the monitor in Datadog                    |
| `$SNAPSHOT`         | URL to a graph snapshot (if applicable)          |
| `$TAGS`            | Comma-separated list of tags                     |
