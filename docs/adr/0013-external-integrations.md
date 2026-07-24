# 0013 — External integrations (PiStat / Hooshnics)

## Status
Accepted

## Context
PiStat remains the primary destination (`DESTINATION_SERVERS`). Hooshnics is an optional async mirror for Laravel ingest. Contracts must stay stable.

## Decision
- PiStat: JSON `{"data":[...filtered records...]}` via `HTTPSender` (retries + rate limit)
- Hooshnics raw: hex envelope JSON to `HOOSHNICS_RAW_URL`
- Hooshnics parsed: same `{"data":[...]}` to `HOOSHNICS_PARSED_URL`
- Failures → Redis retry streams (ADR 0007), not TCP NAK

## Alternatives Considered
- Dual-write sync to both before ACK — rejected (couples ACK to two HTTP APIs)

## Consequences
**Positive:** PiStat path isolated; Hooshnics outage does not stop ingest.  
**Negative:** Mirror can lag; ops watches retry depths.

## References
- `internal/ingest/processor.go`
- `internal/hooshnics/forwarder.go`
- `internal/sender/http_sender.go`
