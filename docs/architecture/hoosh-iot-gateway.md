# Hoosh IoT Gateway — Architecture

**Product name:** Hoosh IoT Gateway (evolved from `gps-data-receiver`)  
**Status:** Phase 1 implementation (device abstraction + parsed-first forwarding + enriched raw durability)  
**Constraint:** Zero-Drop Teltonika path **must not** be redesigned.

---

## 1. Current architecture (as-is)

```
Teltonika Device                    HTTP (Hooshnic JSON / binary)
        |                                    |
        v                                    v
 TCP :5055 ── CRC (CountAVLRecords)     Gin /api ingest
        |                                    |
        v                                    v
 Redis XADD gps:raw_incoming          (legacy / parallel paths)
        |
        v
 TCP ACK (only after XADD)
        |
        v
 Raw consumer group ──► codec.ParsePacket ──► ParsedGPSData
        |                      |
        |                      +──► PiStat HTTPSender (retry / CB / pistat_retry)
        |                      +──► Hooshnics ForwardRaw + ForwardParsed
        |                      +──► Postgres AsyncWriter
        v
 Archive Writer (NDJSON.gz) ──► XACK/XDel
        |
        +──► DLQ / XAUTOCLAIM
```

**Strengths:** ACK-after-Redis, Sentinel HA, retry/DLQ, idempotency, archive, Prometheus.  
**Phase 1 closes:** multi-OEM `device.Driver` registry; enriched raw Redis/archive fields; parsed-first Hooshnics (`HOOSHNICS_FORWARD_RAW=false` by default).  
**Still deferred:** ClickHouse telemetry store; PostGIS entity schema (see §5).

---

## 2. Target architecture (to-be)

```
                    ┌─────────────────────────┐
                    │   Device Driver Registry │
                    │  Teltonika | Hooshnics   │
                    │  (+ future OEMs)         │
                    └────────────┬────────────┘
                                 │ normalize → telemetry.Point
Teltonika TCP                    │
   CRC validate (unchanged)      │
   Redis XADD (enriched raw) ────┤
   ACK                           │
                                 v
                    Raw consumer (async)
                                 |
              ┌──────────────────┼──────────────────┐
              v                  v                  v
         Raw archive      Decode via Driver   ClickHouse (future)
         (disk NDJSON)     → telemetry.Point
                                 |
                    ┌────────────┴────────────┐
                    v                         v
            PiStatForwarder           HooshnicsForwarder
            (parsed JSON)             (parsed JSON; raw optional)
                    |                         |
              retry / CB / DLQ          retry / CB / DLQ

PostgreSQL + PostGIS (future): devices, farms, polygons, users
ClickHouse (future): raw + parsed telemetry history
```

**Invariant:** TCP ACK still depends **only** on Redis `XADD` success. Drivers, archive, Postgres, and forwarders never sit on the ACK hot path.

---

## 3. Data flow (Phase 1)

### 3.1 Raw durability (already async relative to destinations)

1. TCP session: CRC → `EnqueueRawFrame` (`imei`, `frame_hex`, `device_type`, `protocol`, `protocol_version`, `source_ip`, `received_at`, `source`) → ACK  
2. Consumer: optional archive write → decode → destinations  
3. Archive fields mirror Redis stream values (recovery / compliance)

### 3.2 Normalized telemetry (SSoT for forwarding)

`internal/telemetry.Point` is the gateway-normalized model.  
`parser.ParsedGPSData` remains the PiStat wire shape (mapped from `Point` / existing Teltonika conversion — **parse algorithms unchanged**).

### 3.3 Destinations

| Destination | Payload | Reliability |
|-------------|---------|-------------|
| PiStat | `{"data":[ParsedGPSData...]}` | HTTPSender retry + CB + `gps:pistat_retry` + DLQ |
| Hooshnics | **Parsed** by default (`ForwardParsed`) | Forwarder retry + CB + `gps:hooshnics_retry` + DLQ |
| Hooshnics raw | Opt-in `HOOSHNICS_FORWARD_RAW=true` | Same forwarder |

---

## 4. Device driver contract

```go
type Driver interface {
    Name() string
    Detect(raw []byte, hints Hints) bool
    Validate(raw []byte) error
    Decode(raw []byte, hints Hints) (DecodeResult, error) // Points → telemetry.Point
}
```

- **TeltonikaDriver:** wraps `codec.DetectFormat` / `CountAVLRecords` / `ParsePacket` + existing `TeltonikaRecordsToParsed` — **no CRC/parse algorithm edits**.  
- **HooshnicsDriver:** wraps existing `parser.Parse` Hooshnic JSON path.

TCP hot path continues to call `CountAVLRecords` directly (same code Driver.Validate uses) for Zero-Drop latency.

---

## 5. Database evaluation & migration plan (not implemented yet)

### 5.1 Current Postgres usage

Tables used by `internal/storage` (operational, not GIS):

- Success / failed delivery rows (IMEI, raw fragment, parsed JSON, timestamps)
- Invalid parse rows

Suitable for: short-term ops, debugging, small fleets.  
**Not suitable alone for:** multi-year GPS history, heavy aggregations, farm polygon overlays at scale.

### 5.2 Preferred split

| Store | Role |
|-------|------|
| **ClickHouse** | Raw + parsed telemetry time-series (millions+/day), playback, aggregates |
| **PostgreSQL + PostGIS** | Devices, users, farms, polygons, assignments, auth |
| **Redis Streams** | Hot durable ingest + retry/DLQ (unchanged) |
| **Object / NDJSON.gz** | Warm raw archive (existing) |

### 5.3 Migration phases (future)

1. **Freeze** write contracts (`telemetry.Point`, raw envelope fields).  
2. Dual-write parsed points to ClickHouse (async consumer) while Postgres stays for meta.  
3. Move historical queries / playback to ClickHouse.  
4. Introduce PostGIS tables for farms/polygons; API joins device↔farm.  
5. Deprecate bulky GPS history in Postgres.

### 5.4 Impact estimate

| Risk | Mitigation |
|------|------------|
| Dual-write lag | Never on ACK path; backpressure metrics |
| Schema drift | Single `telemetry.Point` encoder |
| Ops cost | ClickHouse HA + retention TTL policies |
| Cutover | Feature flag per destination |

**This phase documents only — no ClickHouse/PostGIS deployment.**

---

## 6. Phase plan

| Phase | Scope | Status |
|-------|--------|--------|
| **0** | Architecture doc + ADR 0024 | Done |
| **1** | Device drivers, telemetry.Point, enriched raw XADD/archive, parsed-first Hooshnics, destination adapters | Done |
| **1.1** | Architecture review (`docs/architecture/phase1-review.md`) | Done |
| **2** | ClickHouse dual-write consumer | Planned |
| **3** | PostGIS entity schema + APIs | Planned |
| **4** | Additional OEM drivers | Planned |

---

## 7. Risks

| Risk | Severity | Notes |
|------|----------|-------|
| Changing Teltonika CRC/parse | Critical | Forbidden without explicit approval |
| Blocking ACK on storage | Critical | Archive/forwarders stay async |
| Breaking PiStat payload shape | High | Keep `ParsedGPSData` wire format |
| Incomplete destination migration | Medium | Dual models until Phase 2 |

---

## 8. Verification

```bash
docker run --rm -v "$PWD:/app" -w /app golang:1.25-alpine \
  go test -mod=vendor ./internal/device/ ./internal/telemetry/ ./internal/destination/ \
  ./internal/archive/ ./internal/teltonika/... -count=1
# Load smoke (Zero-Drop) — requires TCP :5055 up
python ../"GPS Simulator"/teltonika_tcp_tester.py load --host 127.0.0.1 --devices 200 --interval 2 --duration 60 --ramp 15
```

**Phase 1 verification (2026-07-24):** `go test` for device/telemetry/destination/archive/teltonika packages — all **ok**. ACK-after-XADD path unchanged (TCP still calls `EnqueueRawAVL` only).

Expect: no false ACK, pending→0, reconnect storm absent.
