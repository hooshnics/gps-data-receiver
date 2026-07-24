# Alertmanager runbook

## Pipeline

```
Prometheus (rule_files: alerts.yml)
        │
        ▼
Alertmanager (:9093, localhost-bound in compose)
        │
        ▼
Receivers (webhook / email / chat — ops-configured)
```

## Lab defaults

Compose service `alertmanager` loads `monitoring/alertmanager/alertmanager.yml`.

- UI / API: `http://127.0.0.1:9093`
- Default receivers post to `http://127.0.0.1:5001/webhook` (placeholder)
- Critical alerts also route to receiver `critical`

**Before production go-live:** replace webhook URLs with your real ops channel (PagerDuty, Slack, Telegram bot, email SMTP). Do not leave the loopback placeholder as the only sink.

## Verify the chain

1. Confirm Prometheus loads rules: Status → Rules in Prometheus UI (`:9090`).
2. Confirm Alertmanager target: Prometheus → Status → Runtime & Build → Alertmanagers.
3. Fire a test alert (temporary rule or `amtool alert add`).
4. Confirm notification arrives and `send_resolved` works.

## Severity routing

| Severity | Behavior |
|----------|----------|
| `critical` | Group wait 30s; route `critical` + continue to default |
| `warning` | Default receiver; 4h repeat |

Inhibit: when `GPSReceiverDown` fires, related ingest-component noise can be suppressed (see `inhibit_rules`).

## Key alert → runbook map

| Alert | Runbook |
|-------|---------|
| RedisUnavailable / RedisMemoryHigh | [redis-failure.md](./redis-failure.md) |
| RedisSentinelNoMaster / failover | [redis-failover-drill.md](./redis-failover-drill.md) |
| Postgres* | [postgres-failure.md](./postgres-failure.md) |
| RawConsumerLag / Pending* | [worker-crash.md](./worker-crash.md) |
| DeadLetterQueueGrowth | [data-recovery.md](./data-recovery.md) |
| ACKLatencyDegraded / TCP* | [alerts.md](./alerts.md) |
| HostDiskUsageHigh | Expand volume; check `ARCHIVE_DIR` and Redis AOF growth |

## Zero-Drop note

Redis / ACK alerts mean devices may stop receiving ACKs (intentional). Do not “fix” by ACKing before `XADD`.
