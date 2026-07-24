# 0001 — Project microservice architecture

## Status
Accepted

## Context
Hooshnics needs a high-throughput GPS gateway that can ingest Teltonika Codec8/8E TCP traffic and HTTP payloads, fan out to PiStat and Hooshnics, and keep local audit storage — without coupling Laravel/React to device protocols.

## Decision
Run `gps-data-receiver` as a dedicated Go service with:
- TCP server (`:5055`) for native Teltonika
- HTTP API (`:8080`) for legacy/HTTP ingest
- Redis Streams for durable queues
- PostgreSQL for local record audit
- Async workers (consumer groups) for parse/dispatch
- Optional Prometheus/Grafana stack in Docker Compose

## Alternatives Considered
- Parse inside Laravel on every packet — rejected (latency, GC pressure, language mismatch for binary codecs)
- In-memory-only queues — rejected (data loss on crash)
- Monolith inside Hooshtruck backend — rejected (blast radius / deploy cadence)

## Consequences
**Positive:** Clear package boundaries (`teltonika`, `queue`, `ingest`, `hooshnics`, `storage`).  
**Negative:** Operational ownership of Redis/Postgres beside Laravel.  
**Ops:** Deploy via Docker Compose; healthchecks on app/redis/postgres.

## References
- `cmd/server/main.go`
- `docker-compose.yml`
- ADR 0002, 0013
