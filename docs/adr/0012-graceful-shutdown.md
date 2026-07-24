# 0012 — Graceful shutdown

## Status
Accepted

## Context
Deployments must not ACK Redis messages that were only half-processed, and must stop accepting new TCP sessions cleanly.

## Decision
On SIGINT/SIGTERM ordered stop:
1. Teltonika TCP `Stop()` (close listener, wait sessions)
2. Raw consumer `Stop()`
3. Reports consumer `Stop()`
4. Retry consumers `Stop()`
5. HTTP `Shutdown` (30s timeout)

In-flight handlers finish or leave entries pending for XAUTOCLAIM.

Compose: `stop_grace_period: 90s` on `app` so Docker waits for drain.

## Alternatives
- Kill -9 / unordered defer-only stop — risk of unclear drain order

## Consequences
**Positive:** Predictable deploys; pending reclaim covers crash mid-handler.  
**Negative:** Rolling deploy duration includes drain wait.

## Operational Impact
- Checklist: `docs/runbooks/deployment-checklist.md`
- Runbooks: `deployment.md`, `rollback.md`
- Never shorten grace below typical session drain under load

## References
- `cmd/server/main.go` shutdown block
- `docker-compose.yml` `stop_grace_period`
