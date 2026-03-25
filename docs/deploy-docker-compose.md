# Production Deployment with Docker Compose

This guide provides a production-ready `docker-compose.yml` with PostgreSQL and Traefik for TLS termination.

## Prerequisites

- Docker and Docker Compose v2
- A domain name with DNS pointing to your server
- Ports 80 and 443 open for Traefik

## docker-compose.yml

```yaml
services:
  turnis:
    image: ghcr.io/atoolz/turnis:latest
    restart: unless-stopped
    command: ["serve", "--config", "/etc/turnis/turnis.yaml"]
    volumes:
      - turnis-config:/etc/turnis
    environment:
      TURNIS_DATABASE_DRIVER: postgres
      TURNIS_DATABASE_DSN: "postgres://turnis:${POSTGRES_PASSWORD}@postgres:5432/turnis?sslmode=disable"
      TURNIS_SERVER_BASE_URL: "https://${TURNIS_DOMAIN}"
      TURNIS_SLACK_BOT_TOKEN: "${SLACK_BOT_TOKEN}"
      TURNIS_SLACK_APP_TOKEN: "${SLACK_APP_TOKEN}"
      TURNIS_SLACK_SIGNING_SECRET: "${SLACK_SIGNING_SECRET}"
    depends_on:
      postgres:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/healthz"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.turnis.rule=Host(`${TURNIS_DOMAIN}`)"
      - "traefik.http.routers.turnis.entrypoints=websecure"
      - "traefik.http.routers.turnis.tls.certresolver=letsencrypt"
      - "traefik.http.services.turnis.loadbalancer.server.port=8080"
    networks:
      - internal
      - web

  postgres:
    image: postgres:16-alpine
    restart: unless-stopped
    environment:
      POSTGRES_DB: turnis
      POSTGRES_USER: turnis
      POSTGRES_PASSWORD: "${POSTGRES_PASSWORD}"
    volumes:
      - postgres-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U turnis -d turnis"]
      interval: 10s
      timeout: 5s
      retries: 5
      start_period: 10s
    networks:
      - internal

  traefik:
    image: traefik:v3.1
    restart: unless-stopped
    command:
      - "--providers.docker=true"
      - "--providers.docker.exposedbydefault=false"
      - "--providers.docker.network=web"
      - "--entrypoints.web.address=:80"
      - "--entrypoints.web.http.redirections.entrypoint.to=websecure"
      - "--entrypoints.websecure.address=:443"
      - "--certificatesresolvers.letsencrypt.acme.email=${ACME_EMAIL}"
      - "--certificatesresolvers.letsencrypt.acme.storage=/letsencrypt/acme.json"
      - "--certificatesresolvers.letsencrypt.acme.httpchallenge.entrypoint=web"
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - letsencrypt-data:/letsencrypt
    networks:
      - web

volumes:
  postgres-data:
  turnis-config:
  letsencrypt-data:

networks:
  internal:
  web:
    name: web
```

## .env file

Create a `.env` file alongside your `docker-compose.yml`:

```bash
POSTGRES_PASSWORD=change-me-to-a-strong-password
TURNIS_DOMAIN=turnis.example.com
ACME_EMAIL=admin@example.com
SLACK_BOT_TOKEN=xoxb-your-bot-token
SLACK_APP_TOKEN=xapp-your-app-token
SLACK_SIGNING_SECRET=your-signing-secret
```

## Usage

```bash
# Start all services
docker compose up -d

# Check logs
docker compose logs -f turnis

# Stop
docker compose down

# Backup PostgreSQL
docker compose exec postgres pg_dump -U turnis turnis > backup.sql

# Upgrade Turnis
docker compose pull turnis
docker compose up -d turnis
```

## Health Check

Turnis exposes `/healthz` on port 8080. The Docker health check polls this endpoint every 30 seconds. Traefik will only route traffic to healthy containers.

## Volumes

| Volume | Purpose |
|---|---|
| `postgres-data` | PostgreSQL data directory |
| `turnis-config` | Turnis YAML config (mount your config here) |
| `letsencrypt-data` | TLS certificates from Let's Encrypt |

## Twilio and ntfy (optional)

Add these environment variables to the `turnis` service if needed:

```yaml
environment:
  TURNIS_TWILIO_ACCOUNT_SID: "${TWILIO_ACCOUNT_SID}"
  TURNIS_TWILIO_AUTH_TOKEN: "${TWILIO_AUTH_TOKEN}"
  TURNIS_TWILIO_FROM_NUMBER: "${TWILIO_FROM_NUMBER}"
  TURNIS_NTFY_SERVER: "https://ntfy.sh"
```
