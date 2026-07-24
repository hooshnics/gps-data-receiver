# 0021 — Production alerting strategy

## Status
Accepted

## Context
Zero-Drop systems fail *safely* (withhold ACK) but still need paging when Redis, lag, DLQ, ACK latency, or connection pools degrade — otherwise device SD buffers grow unnoticed.

## Decision
Ship Prometheus **recording-free alert rules** in `monitoring/prometheus/alerts.yml`, loaded by `prometheus.yml` `rule_files`.

Critical coverage:
- App / Redis scrape down
- Redis memory > 80% of maxmemory
- Raw stream growth, consumer lag, pending backlog
- DLQ + retry queue growth
- ACK-path Redis write p99 degradation
- TCP active connection spike
- Parser/decode failure spike
- Postgres pool exhaustion

Supporting metrics added where gaps existed:
- `gps_teltonika_active_connections`
- `gps_postgres_*_connections`
- `redis-exporter` scrape job

## Alternatives
- Logs-only paging — rejected (too slow / noisy)
- Full APM traces for every packet — deferred (cost)
- Alerting only on host CPU — insufficient for stream lag / Zero-Drop signals

## Consequences
**Positive:** Actionable pages mapped to runbooks under `docs/runbooks/`.  
**Negative:** Thresholds need tuning per fleet size after first load-test baselines.

## Operational Impact
- Wire Alertmanager / Grafana contact points in environment-specific overlays
- Runbook index: `docs/runbooks/alerts.md`
- Silence narrow windows only during planned Sentinel drills
- Never “fix” alerts by ACKing before Redis

## References
- `monitoring/prometheus/alerts.yml`
- `prometheus.yml`
- `internal/metrics/metrics.go`
- ADR 0010, 0017
