# Production Audit Report — GPS Data Receiver

**Date:** 2026-07-23  
**Scope:** Zero-Drop ingestion, Redis HA consumers, TCP, DLQ, idempotency, replay, config  
**Priority:** ZERO DATA LOSS · NO ACK WITHOUT DURABILITY · NO SILENT DROP

---

## 1. Zero-Drop invariant (TCP path)

**Verified in** `internal/teltonika/tcp/session.go`:

```
readFrame → CountAVLRecords (CRC) → EnqueueRawAVL (XADD) → writeAck(count)
```

| Check | Result |
|-------|--------|
| ACK before XADD | **None found** |
| ACK on XADD error | **No** — session returns (connection closes); device retains/resends |
| CRC fail | ACK `0` (protocol reject), no XADD |
| Panic before ACK decision | No panic on hot path; write errors abort without false success |

**Accepted risk:** Redis `XADD` success with AOF `everysec` means command accepted into Redis memory/AOF buffer, not necessarily fsynced that millisecond. Replica + Sentinel cover availability; un-ACKed frames stay on device for true loss windows.

---

## 2. Bugs discovered and fixed

### CRITICAL — Silent post-ACK drop via dedupe claim
**Risk:** `ClaimFirstSighting` ran before handler success. On handler failure, message stayed in PEL; reclaim saw duplicate hash and ACKed without processing.  
**Why it matters:** Device already had TCP ACK (data in Redis), but downstream work could vanish.  
**Fix:** `ReleaseFirstSighting` on handler error; `ReleaseTelemetrySighting` when PiStat park fails after telemetry claims.  
**Files:** `durable.go`, `raw_consumer.go`, `processor.go` · ADR 0022

### HIGH — DLQ failure still ACKed
**Risk:** Invalid `frame_hex` / decode / retry-exhaustion paths ACKed even if DLQ `XADD` failed → poison lost with no DLQ copy.  
**Fix:** ACK only after successful DLQ write; else leave pending.  
**Files:** `raw_consumer.go`, `processor.go`, `retry_consumer.go`

### MEDIUM — Unbounded TCP sessions
**Risk:** Accept storm → memory/goroutine growth.  
**Fix:** `TELTONIKA_MAX_CONNECTIONS` (default 10000); excess connections closed.  
**Files:** `server.go`, `config.go`

### MEDIUM — Production unsafe defaults
**Risk:** Empty IMEI whitelist + default Postgres password in `ENVIRONMENT=production`.  
**Fix:** Config refuses start unless `TELTONIKA_ALLOW_ANY_IMEI` / `POSTGRES_ALLOW_DEFAULT_PASSWORD` explicitly set (compose lab sets them).  
**Files:** `config.go`, `docker-compose.yml`, `env.example`

### MEDIUM — Replay could target sensitive streams
**Risk:** Enqueue only blocked exact live raw name.  
**Fix:** `GuardReplayTarget` blocks `raw_incoming`, `reports`, DLQ, retry streams; production reprocess with externals requires `-confirm-production`.  
**Files:** `replay/source.go`, `cmd/replay/main.go`

### LOW — Raw worker panic could kill worker loop
**Fix:** recover wrappers aligned with reports consumer.

### LOW — DLQ missing attempt metadata on retry exhaust
**Fix:** `EnqueueDeadLetterAttempt` stores `attempt`.

---

## 3. Areas verified OK (no code change)

| Area | Notes |
|------|-------|
| Sentinel client | `NewFailoverClient` when `REDIS_SENTINEL_ADDRS` set; XADD errors withhold ACK |
| Consumer order | Process → (success) XACK+XDEL; failure leaves PEL; XAUTOCLAIM 60s |
| Reports consumer | Concurrent batch + ACK after handler success |
| Postgres after ACK | Failure cannot lose device data; async writer + UNIQUE `dedupe_key` |
| Replay dry-run | Default safe; no live stream mutation |
| Metrics | Active TCP, packets, Redis write hist, pending, depths, DLQ, Postgres pool |

---

## 4. Files inspected (primary)

- `internal/teltonika/tcp/session.go`, `server.go`
- `internal/queue/durable.go`, `redis_queue.go`, `raw_consumer.go`, `retry_consumer.go`, `consumer.go`
- `internal/ingest/processor.go`
- `internal/storage/postgres.go`
- `internal/replay/*`, `cmd/replay/main.go`
- `internal/config/config.go`, `cmd/server/main.go`
- `docker-compose.yml`, `docker/redis/*`, ADRs 0002–0022

---

## 5. Tests executed

- `go test` (with `-race` where CGO available) on `internal/queue`, `ingest`, `teltonika/...`, `replay`, `config`
- `go build` `cmd/server`, `cmd/replay`

---

## 6. Remaining accepted risks

1. **AOF everysec** — sub-second loss window on hard power loss after XADD returns; mitigated by device un-ACK retransmission if connection drops before ACK write completes; if ACK was sent and both Redis+replica die before fsync, theoretical loss (rare).
2. **Async replication lag** during Sentinel failover — brief XADD failures (correct Zero-Drop behavior).
3. **Crash between claim and release** — narrow window; PEL retry + ops monitoring.
4. **Postgres pool / destination outage** — does not affect TCP ACK; retry/DLQ absorb.
5. **Full `go test -race ./...` on Windows host** — use Linux/CI with CGO; alpine image used in audit.

---

## 7. Recommended next phase

1. Wire Alertmanager receivers to pages for critical rules
2. Optional archive consumer group (copy-before-XDEL) for cold retention
3. Chaos drill: kill Redis primary under simulator load; document failover gap
4. Fill `docs/performance/load-test-report.md` with measured p50/p95/p99 on target hardware
5. Consider optional `REDIS_XADD_WAIT_REPLICAS=1` for stricter durability (latency tradeoff) behind a flag
