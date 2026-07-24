# Phase 1.1 — Hoosh IoT Gateway Architecture Review

**Scope:** Review of Phase 1 implementation prior to ClickHouse dual-write (Phase 2).  
**Constraint:** Do not redesign Zero-Drop (`CRC → Redis XADD → ACK`). Fix only production-scalability architectural issues.  
**Date:** 2026-07-24

---

## Verdict

Phase 1 is **production-capable** for Teltonika Zero-Drop ingest. Several gateway-product gaps were found; the **scalability-critical** ones were fixed in this review pass. Remaining items are Phase 2+ refinements, not blockers.

| Check | Status | Notes |
|-------|--------|-------|
| 1. Canonical `telemetry.Point` | **Fixed** | PiStat mapping moved to `destination` |
| 2. Parser → Point → Adapters | **Pass** (with adapter boundary) | Drivers → Point → destination map → PiStat/Hooshnics |
| 3. Raw storage / ACK | **Pass** | ACK unchanged; archive failures now metric + retry/DLQ |
| 4. Replay design | **Documented + wired** | Archive/Redis → `cmd/replay` → driver pipeline |
| 5. Gateway metrics | **Added** | Requested series exposed |
| 6. Memory / goroutines @ 200–1000 devices | **Pass** after AsyncWriter fix | See §6 |

---

## 1. Canonical telemetry model

### Finding (pre-fix)

`internal/telemetry.Point` claimed to be SSoT but:

- imported `parser` and owned `ToParsedGPSData` / `FromParsedGPSData`
- carried `WallClock` documented as PiStat-specific
- ingest converted Point → `ParsedGPSData` immediately and treated PiStat shape as the working model

That couples Phase 2 ClickHouse / future OEM adapters to PiStat wire format.

### Fix applied

- `telemetry.Point` is destination-agnostic (`Timestamp` UTC + optional `DeviceClock`).
- PiStat mapping lives in `internal/destination/pistat.go` (`ToPiStatRecords` / `FromPiStatRecords`).
- Ingest builds `[]telemetry.Point` first, then crosses the adapter boundary.

**Invariant for Phase 2:** dual-write ClickHouse from `[]telemetry.Point` only — never from `ParsedGPSData`.

---

## 2. Parser flow

### Target

```
Device Driver (Detect/Validate/Decode)
        ↓
telemetry.Point  (canonical)
        ↓
Destination Adapters (PiStat / Hooshnics / future CH)
```

### Current (post Phase 1.1)

```
TCP CRC (CountAVLRecords) ── unchanged hot path
        ↓
Redis XADD (enriched raw) ── ACK only here
        ↓
Raw consumer
        ↓
device.Registry.Decode → telemetry.Point
        ↓
destination.ToPiStatRecords  (adapter)
        ↓
StoppageFilter (still ParsedGPSData — legacy)
        ↓
HooshnicsParsedForwarder / HTTPSender (PiStat)
```

**Pass:** canonical Point exists before destinations.  
**Deferred (non-blocking):** StoppageFilter still operates on PiStat records; can accept `[]Point` in a later cleanup without touching ACK.

Replay reprocess now wires `device.DefaultRegistry` so offline path matches live decode.

---

## 3. Raw storage & ACK

### ACK path (unchanged — required)

```
readFrame → CountAVLRecords → EnqueueRawAVL (XADD) → writeAck
```

- Redis XADD failure → **no ACK**, session kept (retransmit).
- Archive / parse / forward never on TCP ACK path.

### Async raw persistence

| Layer | Role | Failure behavior |
|-------|------|------------------|
| Redis `gps:raw_incoming` | Primary durable raw | No ACK; device retry |
| Local NDJSON.gz archive | Warm secondary | Strict: leave pending; Non-strict: **DLQ park** + ACK (recoverable) |
| Parse/dispatch | Downstream | Pending / pistat_retry / DLQ |

### Fix applied

Non-strict archive failure previously ACKed with only a log (silent warm-archive loss). Now:

1. `raw_storage_failures_total` incremented  
2. Frame parked on `gps:dead_letter` (or pending if DLQ park fails)  
3. Strict mode still leaves message pending (retry)

TCP ingest remains unblocked.

---

## 4. Replay capability design

### Goal

Any locally stored raw packet must be replayable through the same parser pipeline.

### Sources

| Source | Tooling |
|--------|---------|
| Redis `gps:raw_incoming` (XRANGE, read-only) | `cmd/replay -source redis` |
| `gps:dead_letter` | `cmd/replay -source dlq` |
| Archive NDJSON.gz | `cmd/replay -source file` |

### Modes (existing)

| Mode | Behavior |
|------|----------|
| `dry-run` | CRC + codec parse report (no writes) |
| `export` | NDJSON dump |
| `enqueue` | XADD to non-live target (e.g. `gps:raw_replay`) |
| `reprocess` | `Processor.ProcessRawAVLWithOptions` with Drivers |

### Safety

- Never XACK/XDEL live consumer-group entries from replay CLI  
- Production guard for `-no-external=false`  
- Target stream must not be the live raw stream  

### Entry contract (replayable envelope)

```
imei, frame_hex, received_at, source,
device_type, protocol, protocol_version, source_ip
```

Archive writer and Redis XADD already emit these; replay `Entry` + Redis reader updated to round-trip them.

### Recommended ops flow

1. Identify gap / bad parse → export archive slice or DLQ  
2. `dry-run` validate  
3. `reprocess -force` after parser fix (SkipDedupe) with `-no-external` first  
4. Optionally enqueue to shadow stream for consumer soak  

---

## 5. Metrics

### Gateway series (Phase 1.1)

| Metric | Meaning |
|--------|---------|
| `received_packets_total` | Framed TCP packets (also `gps_teltonika_packets_received_total`) |
| `parsed_packets_total` | Decoded telemetry points |
| `parse_errors_total` | Decode failures / zero-record |
| `redis_stream_lag` | Raw consumer-group **pending** (lag proxy) |
| `raw_storage_failures_total` | Redis XADD failures + archive write failures |
| `forwarding_failures_total{destination}` | PiStat / Hooshnics forward failures |

Legacy `gps_*` metrics remain for dashboards; do not remove.

### Alert suggestions

- `rate(raw_storage_failures_total[5m]) > 0`  
- `redis_stream_lag > N` (tune to worker capacity)  
- `rate(forwarding_failures_total[5m])` by destination  
- `gps_postgres_async_queue_drops_total` rising → scale writer / raise queue  

---

## 6. Memory growth & goroutines (200–1000 devices)

### Expected steady-state

| Component | Scaling | Bound |
|-----------|---------|-------|
| TCP session goroutine | 1 / connection | ≈ device count |
| Raw + reports workers | `WORKER_COUNT` | Fixed |
| Hooshnics workers | `HOOSHNICS_WORKERS` | Fixed + buffered chan (default 50k) |
| Stoppage filter | map per IMEI | Fleet size (sharded) |
| Postgres AsyncWriter | 1 goroutine + chan | Fixed |

At 1000 devices × ~1 pkt / few seconds, Redis XADD stays on critical path; consumers absorb bursts via stream depth.

### Critical issue found & fixed

`storage.AsyncWriter` previously spawned a **new goroutine per overflow enqueue** when the Postgres queue was full. Under destination + DB pressure this unbounded spawn → memory growth / goroutine leak.

**Fix:** drop with warn + `gps_postgres_async_queue_drops_total` (no spawn). Delivery path already durable in Redis retry streams.

### Residual risks (monitor, not redesign)

1. **Hooshnics buffer 50k** — full → park Redis retry (good); watch spill disk.  
2. **Socket.IO broadcaster** — large payloads on every delivery can amplify CPU/RAM; keep sampling / disable in high load if needed.  
3. **Per-connection goroutine** — 1k is fine; 10k+ may need accept limits / SOMAXCONN tuning (ops, not code redesign).  
4. **Filter state maps** — grow with unique IMEIs; reclaim idle IMEIs later if multi-tenant fleets explode.

---

## 7. Changes made in this review (non-redesign)

1. Decouple `telemetry.Point` from PiStat (`destination/pistat.go`).  
2. Ingest: Driver → Point → adapter map; Hooshnics via `HooshnicsParsedForwarder`.  
3. Gateway Prometheus series listed in §5.  
4. Archive failure → metric + DLQ (non-strict) / pending (strict).  
5. AsyncWriter: remove unbounded overflow goroutines.  
6. Replay: enriched Entry; reprocess uses device registry.  

**Not changed:** Teltonika CRC/parse algorithms, ACK-after-XADD, Redis Streams topology, retry/CB design.

---

## 8. Phase 2 readiness checklist

- [x] Canonical Point free of PiStat package import  
- [x] Raw ACK path isolated  
- [x] Replay path through Drivers  
- [x] Core gateway metrics  
- [ ] ClickHouse async consumer writing `telemetry.Point` (Phase 2)  
- [ ] Optional: StoppageFilter on `[]Point`  
- [ ] Optional: PiStatForwarder.Send with delivery observers (thin wrapper today)

---

## References

- `docs/architecture/hoosh-iot-gateway.md`  
- `docs/adr/0024-hoosh-iot-gateway-device-abstraction.md`  
- `docs/adr/0004-ack-after-durable-write.md`  
- `docs/adr/0023-data-archive-implementation.md`  
- `cmd/replay/main.go`  
- `internal/telemetry/point.go`  
- `internal/destination/`  
- `internal/ingest/processor.go`  
- `internal/storage/async_writer.go`  
- `internal/queue/raw_consumer.go`  
- `internal/metrics/metrics.go`
