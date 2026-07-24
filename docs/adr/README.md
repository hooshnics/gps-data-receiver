# ADR Index — GPS Data Receiver

## Status
Accepted

## Purpose
This directory is the single source of truth for architectural decisions in `gps-data-receiver`.
ADRs describe **what is implemented**, not aspirational design.

## Index

| ID | Title | Status |
|----|-------|--------|
| 0001 | Project / microservice architecture | Accepted |
| 0002 | Zero-drop ingestion | Accepted |
| 0003 | Redis Streams as durable backbone | Accepted |
| 0004 | ACK only after durable Redis XADD | Accepted |
| 0005 | Consumer groups | Accepted |
| 0006 | Dead-letter queue | Accepted |
| 0007 | Retry queues | Accepted |
| 0008 | Idempotency | Accepted |
| 0009 | PostgreSQL persistence | Accepted |
| 0010 | Observability | Accepted |
| 0011 | Docker & Redis AOF | Accepted |
| 0012 | Graceful shutdown | Accepted |
| 0013 | External integrations (PiStat / Hooshnics) | Accepted |
| 0014 | Backpressure & retention | Accepted |
| 0015 | Security assumptions | Accepted |
| 0016 | Future scaling | Accepted |
| 0017 | Redis high availability (Sentinel) | Accepted |
| 0018 | Replay and data recovery | Accepted |
| 0019 | Load testing strategy | Accepted |
| 0020 | Raw data retention strategy | Accepted |
| 0021 | Production alerting strategy | Accepted |
| 0022 | Post-ACK dedupe claim safety | Accepted |
| 0023 | Data archive implementation | Accepted |
| 0024 | Hoosh IoT Gateway device abstraction | Accepted |

## References
- `cmd/server/main.go`
- `cmd/teltonika-simulator/`
- `cmd/ack-latency-bench/`
- `cmd/replay/`
- `internal/teltonika/tcp/`
- `internal/queue/`
- `internal/ingest/`
- `internal/replay/`
- `internal/archive/`
- `monitoring/prometheus/alerts.yml`
- `monitoring/alertmanager/alertmanager.yml`
- `docs/runbooks/`
- `docker-compose.yml`
- `docker/redis/`
- `docs/performance/load-test-report.md`
- `docs/recovery/`
- `docs/security/security-review.md`
- `docs/architecture/scaling-strategy.md`
- `docs/architecture/hoosh-iot-gateway.md`
- `internal/device/`
- `internal/telemetry/`
- `internal/destination/`
- `env.example`
- `env.production.example`
