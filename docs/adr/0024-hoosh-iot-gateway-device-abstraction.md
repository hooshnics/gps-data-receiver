# ADR 0024 — Hoosh IoT Gateway device abstraction

## Status

Accepted (Phase 1)

## Context

The GPS receiver must evolve into a multi-manufacturer **Hoosh IoT Gateway** without rewriting Zero-Drop ingest or Teltonika CRC/parse algorithms.

## Decision

1. Introduce `internal/device.Driver` (Detect / Validate / Decode) with Teltonika + Hooshnics adapters wrapping existing packages.
2. Introduce `internal/telemetry.Point` as normalized SSoT for forwarders.
3. Enrich Redis raw `XADD` + archive with `device_type`, `protocol`, `source_ip` (async consumers only for archive).
4. Default `HOOSHNICS_FORWARD_RAW=false` (parsed-only mirror); legacy raw restored via flag.
5. Thin `internal/destination` adapters over existing Hooshnics/PiStat clients (retry/CB/DLQ unchanged).
6. ClickHouse + PostGIS documented as future migration — **not** implemented in this phase.

## Consequences

**Positive:** Clear extension point for new OEMs; gateway product narrative; safer destination contract.  
**Negative:** Temporary dual models (`ParsedGPSData` + `telemetry.Point`) until destinations fully migrate.  
**Ops:** See `docs/architecture/hoosh-iot-gateway.md`.

## References

- `docs/architecture/hoosh-iot-gateway.md`
- `internal/device/`, `internal/telemetry/`, `internal/destination/`
