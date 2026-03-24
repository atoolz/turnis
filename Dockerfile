FROM golang:1.23-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /turnis ./cmd/turnis

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 1000 turnis && \
    mkdir -p /data /etc/turnis && \
    chown turnis:turnis /data
COPY --from=builder /turnis /usr/local/bin/turnis

USER turnis
EXPOSE 8080
VOLUME ["/data"]
ENV TURNIS_DATABASE_DSN=/data/turnis.db

ENTRYPOINT ["turnis"]
CMD ["serve", "--config", "/etc/turnis/turnis.yaml"]
