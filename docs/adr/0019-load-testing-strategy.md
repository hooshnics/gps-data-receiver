# 0019 — Load testing strategy

## Status
Accepted

## Context
Fleet growth (1k–10k devices) requires evidence that the Zero-Drop path sustains target packet rates with acceptable ACK latency under Redis AOF `everysec`, without moving ACK before durable write.

## Decision
Adopt a **two-tool** load validation approach:

| Tool | Path | Measures |
|------|------|----------|
| `cmd/teltonika-simulator` | Full TCP fleet | PPS, ACK p50/p95/p99, reconnect, offline buffer, Codec8/8E |
| `cmd/ack-latency-bench` | Critical path | TCP end-to-end ACK RTT; local CRC+XADD microbench |

Scenarios documented in `docs/performance/load-test-report.md` (1000/5000/10000 devices).  
Correlate with Prometheus: stream depth, pending, ingest errors, Redis write histogram, host CPU/RAM.

**Rule:** Do not optimize the ACK path without measured p99 evidence.

## Alternatives
- HTTP-only load tests — insufficient for Teltonika TCP session realism
- Production canary alone — too late for capacity planning
- Synthetic Redis-only benchmarks without TCP — useful microbench but not fleet behavior

## Consequences
**Positive:** Repeatable capacity evidence; separates TCP stack vs Redis write cost.  
**Negative:** Large device counts need ramp (`-ramp`) and adequate host file descriptors.

## Operational Impact
- Run before major Redis/app upgrades and after hardware changes
- Fill results template in `docs/performance/load-test-report.md`
- Makefile: `teltonika-sim`, `ack-latency-bench`
- Pair with Sentinel failover drill under moderate simulator load

## References
- `cmd/teltonika-simulator/`, `cmd/ack-latency-bench/`
- `docs/performance/load-test-report.md`
- ADR 0002, 0004, 0010
