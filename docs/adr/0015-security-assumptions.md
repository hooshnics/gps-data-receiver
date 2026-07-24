# 0015 — Security assumptions

## Status
Accepted

## Context
GPS ingest endpoints are network-exposed; tokens and IMEI filters reduce abuse.

## Decision
- Hooshnics mirror requires `HOOSHNICS_AUTH_TOKEN` (`X-Ingest-Token`)
- Optional `TELTONIKA_IMEI_WHITELIST`
- HTTP rate limiting on ingress
- Compose binds Redis/Postgres to localhost by default; app `no-new-privileges`
- Do not log full binary payloads at info level

## Alternatives Considered
- mTLS for every device — future; not all Teltonika fleets support it today

## Consequences
**Positive:** Baseline auth/rate-limit.  
**Negative:** TCP `:5055` authenticity is IMEI-based, not cryptographic — network controls required.

## References
- `internal/config/config.go`
- `internal/hooshnics/forwarder.go`
- `docker-compose.yml`
