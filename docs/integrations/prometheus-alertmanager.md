# Prometheus Alertmanager Integration

This guide configures Alertmanager to send alerts to Turnis via its webhook endpoint.

## Prerequisites

- A running Turnis instance with a webhook integration created (see [Getting Started](../getting-started.md#7-create-a-webhook-integration))
- Your webhook token from the integration response
- Prometheus Alertmanager v0.20+

## Configure Alertmanager

Alertmanager's native payload format is a nested structure that Turnis does not parse directly. You need a lightweight adapter (shown below) to transform Alertmanager payloads into Turnis's flat webhook format.

### Option 1: With adapter (recommended)

The adapter runs as a sidecar or separate container. See the full adapter code and Docker Compose example below.

### Option 2: Direct webhook (requires custom template)

If you prefer no adapter, configure Alertmanager with a custom `webhook_configs` template. Note: the default Alertmanager webhook payload will NOT work without a template that transforms it.

### Full alertmanager.yml example (with adapter)

```yaml
global:
  resolve_timeout: 5m

route:
  receiver: turnis
  group_by: ['alertname', 'instance']
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 4h

  routes:
    # Critical alerts go to Turnis immediately
    - match:
        severity: critical
      receiver: turnis
      group_wait: 10s
      repeat_interval: 1h

    # Warning alerts go to Turnis with default timing
    - match:
        severity: warning
      receiver: turnis

receivers:
  - name: turnis
    webhook_configs:
      - url: 'https://turnis.yourcompany.com/webhook/YOUR_WEBHOOK_TOKEN'
        send_resolved: false
        max_alerts: 0
```

Replace `YOUR_WEBHOOK_TOKEN` with the token from your Turnis integration.

**`send_resolved: false`** is recommended because Alertmanager's resolve payload does not map cleanly to a new Turnis alert. Resolve alerts through Turnis directly (via Slack, the web UI, or the REST API).

If you set `send_resolved: true`, Alertmanager will fire another webhook when the alert resolves. Turnis will deduplicate it if the fingerprint matches an active alert, but it will not auto-resolve the existing alert.

## How the payload mapping works

Alertmanager sends a JSON payload with a list of alerts. Each alert contains `labels`, `annotations`, `status`, `startsAt`, and other fields. Turnis does not parse the native Alertmanager format directly. You need a small adapter, or you can use Alertmanager's built-in templating via `webhook_configs` with a custom HTTP body.

However, the simplest approach is to use an Alertmanager webhook receiver with a **custom template** that transforms the payload into Turnis format.

### Option 1: Use alertmanager-webhook-adapter (recommended)

Deploy a lightweight adapter that translates Alertmanager's native payload into Turnis format. Here is a minimal adapter using a shell script and `socat`, or use any HTTP proxy that can transform JSON.

A more practical approach: use Alertmanager's `webhook_configs` with an intermediate proxy. But the easiest path is configuring your alerts with clear labels and using the mapping table below.

### Option 2: Direct integration with label conventions

Turnis accepts any JSON that matches the [generic webhook schema](generic-webhook.md). The key is to construct a fingerprint that Alertmanager sends consistently. Alertmanager already generates a fingerprint for each alert group, but it sends the native format.

The recommended approach is to place a thin HTTP adapter between Alertmanager and Turnis. Here is a complete example using a Go adapter:

```go
// cmd/am-turnis-adapter/main.go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

type alertmanagerPayload struct {
	Alerts []amAlert `json:"alerts"`
}

type amAlert struct {
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	Fingerprint string            `json:"fingerprint"`
}

type turnisAlert struct {
	Title       string            `json:"title"`
	Message     string            `json:"message,omitempty"`
	Severity    string            `json:"severity,omitempty"`
	Source      string            `json:"source"`
	Fingerprint string            `json:"fingerprint"`
	Labels      map[string]string `json:"labels,omitempty"`
}

func main() {
	turnisURL := os.Getenv("TURNIS_WEBHOOK_URL")
	if turnisURL == "" {
		log.Fatal("TURNIS_WEBHOOK_URL is required")
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		var payload alertmanagerPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		for _, a := range payload.Alerts {
			if a.Status == "resolved" {
				continue // skip resolved, handle via Turnis directly
			}

			ta := turnisAlert{
				Title:       a.Labels["alertname"],
				Message:     a.Annotations["description"],
				Severity:    mapSeverity(a.Labels["severity"]),
				Source:      "prometheus",
				Fingerprint: a.Fingerprint,
				Labels:      a.Labels,
			}

			body, _ := json.Marshal(ta)
			resp, err := http.Post(turnisURL, "application/json", bytes.NewReader(body))
			if err != nil {
				log.Printf("failed to forward alert: %v", err)
				continue
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			log.Printf("forwarded alert=%s status=%d", a.Labels["alertname"], resp.StatusCode)
		}

		w.WriteHeader(http.StatusOK)
	})

	addr := ":9097"
	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func mapSeverity(s string) string {
	switch strings.ToLower(s) {
	case "critical", "error", "fatal":
		return "critical"
	case "warning", "warn":
		return "warning"
	default:
		return "info"
	}
}
```

Build and run:

```bash
go build -o am-turnis-adapter ./cmd/am-turnis-adapter
TURNIS_WEBHOOK_URL=https://turnis.yourcompany.com/webhook/YOUR_WEBHOOK_TOKEN ./am-turnis-adapter
```

Then point Alertmanager at the adapter instead of Turnis directly:

```yaml
receivers:
  - name: turnis
    webhook_configs:
      - url: 'http://am-turnis-adapter:9097'
        send_resolved: false
```

### Label to Turnis field mapping

| Alertmanager field             | Turnis field    | Notes                                                     |
|--------------------------------|-----------------|-----------------------------------------------------------|
| `labels.alertname`             | `title`         | Alert name becomes the title                              |
| `annotations.description`     | `message`       | Long description or runbook link                          |
| `labels.severity`             | `severity`      | Mapped to `info`, `warning`, or `critical`                |
| `fingerprint`                 | `fingerprint`   | Alertmanager's auto-generated fingerprint, used as-is     |
| All `labels`                  | `labels`        | Passed through as key-value pairs                         |
| (hardcoded)                   | `source`        | Set to `"prometheus"` by the adapter                      |

## What Alertmanager sends (native format)

For reference, here is the JSON payload Alertmanager sends to webhook receivers:

```json
{
  "version": "4",
  "groupKey": "{}:{alertname=\"HighCPU\", instance=\"api-server-01:9090\"}",
  "truncatedAlerts": 0,
  "status": "firing",
  "receiver": "turnis",
  "groupLabels": {
    "alertname": "HighCPU",
    "instance": "api-server-01:9090"
  },
  "commonLabels": {
    "alertname": "HighCPU",
    "severity": "critical",
    "instance": "api-server-01:9090",
    "job": "node-exporter"
  },
  "commonAnnotations": {
    "description": "CPU usage on api-server-01 exceeded 90% for 5 minutes.",
    "summary": "High CPU on api-server-01"
  },
  "externalURL": "http://alertmanager.example.com",
  "alerts": [
    {
      "status": "firing",
      "labels": {
        "alertname": "HighCPU",
        "severity": "critical",
        "instance": "api-server-01:9090",
        "job": "node-exporter"
      },
      "annotations": {
        "description": "CPU usage on api-server-01 exceeded 90% for 5 minutes.",
        "summary": "High CPU on api-server-01"
      },
      "startsAt": "2026-03-25T14:20:00.000Z",
      "endsAt": "0001-01-01T00:00:00Z",
      "generatorURL": "http://prometheus.example.com/graph?g0.expr=...",
      "fingerprint": "e6b04b7cc3b97b2a"
    }
  ]
}
```

After the adapter transforms it, Turnis receives:

```json
{
  "title": "HighCPU",
  "message": "CPU usage on api-server-01 exceeded 90% for 5 minutes.",
  "severity": "critical",
  "source": "prometheus",
  "fingerprint": "e6b04b7cc3b97b2a",
  "labels": {
    "alertname": "HighCPU",
    "severity": "critical",
    "instance": "api-server-01:9090",
    "job": "node-exporter"
  }
}
```

## Docker Compose example with the adapter

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

  am-turnis-adapter:
    build: ./cmd/am-turnis-adapter
    environment:
      TURNIS_WEBHOOK_URL: http://turnis:8080/webhook/YOUR_WEBHOOK_TOKEN
    ports:
      - "9097:9097"

  alertmanager:
    image: prom/alertmanager:latest
    volumes:
      - ./alertmanager.yml:/etc/alertmanager/alertmanager.yml
    ports:
      - "9093:9093"

volumes:
  turnis-data:
```

## Verifying the integration

1. Create a test alert in Prometheus or fire one manually via the Alertmanager API:

```bash
curl -s -X POST http://localhost:9093/api/v2/alerts \
  -H "Content-Type: application/json" \
  -d '[{
    "labels": {
      "alertname": "TestAlert",
      "severity": "warning",
      "instance": "test-server:9090"
    },
    "annotations": {
      "description": "This is a test alert from Alertmanager."
    }
  }]'
```

2. Check Turnis for the new alert:

```bash
curl -s http://localhost:8080/api/v1/alerts | jq
```

You should see an alert with `title: "TestAlert"` and `source: "prometheus"`.
