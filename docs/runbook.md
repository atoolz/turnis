# Operator Runbook

## Backup and Restore

### SQLite

SQLite is a single file. Stop Turnis, copy the database, and start again.

```bash
# Backup
cp turnis.db turnis.db.bak

# Restore
cp turnis.db.bak turnis.db
```

If Turnis is running with WAL mode (the default), also copy the WAL and SHM files if they exist:

```bash
cp turnis.db-wal turnis.db-wal.bak
cp turnis.db-shm turnis.db-shm.bak
```

For a consistent backup without stopping Turnis, use the SQLite `.backup` command:

```bash
sqlite3 turnis.db ".backup turnis-backup.db"
```

### PostgreSQL

```bash
# Backup
pg_dump -Fc turnis > turnis_$(date +%Y%m%d_%H%M%S).dump

# Restore
pg_restore -d turnis turnis_20240101_120000.dump
```

## Upgrading

1. Stop the running Turnis process.
2. Back up the database (see above).
3. Replace the `turnis` binary with the new version.
4. Start Turnis. Migrations run automatically on startup, no manual SQL needed.

```bash
systemctl stop turnis
cp turnis.db turnis.db.pre-upgrade
cp /path/to/new/turnis /usr/local/bin/turnis
systemctl start turnis
journalctl -u turnis -f  # watch for migration log lines
```

## Rotating API Keys

Turnis API keys are managed via the CLI. The process is: create a new key, update any integrations or scripts that use the old key, then delete the old one.

```bash
# List existing keys
turnis keys list

# Create a new key
turnis keys create --name "monitoring-v2"

# Update your monitoring tool to use the new token, then delete the old one
turnis keys delete <old-key-id>
```

## Rotating Slack and Twilio Tokens

Slack bot tokens, app tokens, and Twilio credentials are set in the config file or environment variables. To rotate:

1. Generate new credentials in the respective platform (Slack App settings, Twilio console).
2. Update `turnis.yaml` or the corresponding environment variables:

```yaml
slack:
  bot_token: "xoxb-new-token"
  app_token: "xapp-new-token"
  signing_secret: "new-secret"

twilio:
  account_sid: "ACnewsid"
  auth_token: "new-auth-token"
  from_number: "+15551234567"
```

Or via environment:

```bash
export TURNIS_SLACK_BOT_TOKEN=xoxb-new-token
export TURNIS_SLACK_APP_TOKEN=xapp-new-token
export TURNIS_TWILIO_AUTH_TOKEN=new-auth-token
```

3. Restart Turnis for the changes to take effect.

```bash
systemctl restart turnis
```

## Diagnosing Failed Escalations

### Check delivery attempts in the database

```sql
-- Recent failed deliveries
SELECT id, alert_id, user_id, channel, address, failure_reason, retry_count, dispatched_at, failed_at
FROM delivery_attempts
WHERE failed_at IS NOT NULL
ORDER BY dispatched_at DESC
LIMIT 20;

-- Deliveries for a specific alert
SELECT *
FROM delivery_attempts
WHERE alert_id = '<alert-id>'
ORDER BY dispatched_at;
```

### Check logs

Turnis logs escalation events with structured fields. Filter by alert_id to trace the full lifecycle.

```bash
# Systemd journal
journalctl -u turnis | grep 'alert_id=<id>'

# Docker
docker logs turnis 2>&1 | grep 'alert_id=<id>'
```

Key log messages to look for:

- `escalation: notifying user` confirms the engine attempted notification.
- `notification dispatch failed` shows sender-level failures.
- `retrying notification dispatch` shows retry attempts with backoff duration.
- `notification dispatch permanently failed after retries` means all retries were exhausted.
- `escalation: step timed out, escalating` means the step timeout elapsed without acknowledgment.
- `escalation: exhausted all steps` means the policy ran through all steps and repeats.

## Channel Down Recovery

When a notification channel (Slack, Twilio, SMTP) is unreachable, Turnis applies the following retry behavior:

**Transient errors (retried automatically):**
- Network timeouts
- Connection refused / connection reset
- HTTP 5xx responses from the provider

The dispatcher retries up to 3 times with exponential backoff: 1s, 4s, 16s (plus random 0-25% jitter). Each retry updates the `retry_count` column in `delivery_attempts`.

**Non-transient errors (not retried):**
- HTTP 4xx responses (bad request, unauthorized, forbidden)
- Invalid addresses (malformed email, missing phone number)
- Missing sender configuration

After all retries are exhausted, the delivery is marked as permanently failed and the escalation engine continues to the next step in the policy. This means if Slack is fully down, the escalation will eventually reach an SMS or voice step if the policy is configured with multiple channels.

**Recovery checklist:**

1. Check the provider's status page (status.slack.com, status.twilio.com).
2. Query `delivery_attempts` for `retry_count > 0` to see which deliveries hit retries.
3. Once the channel recovers, new alerts will be delivered normally. Turnis does not re-deliver failed past notifications automatically.
4. For critical alerts that were missed, manually re-fire them or acknowledge/resolve as appropriate.
