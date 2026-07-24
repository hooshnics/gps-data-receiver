# Runbook: Worker crash / consumer lag

## Symptoms
- `gps_raw_pending_count` rising
- `gps_raw_stream_depth` rising while ACKs continue (ingest healthy, processing stuck)
- Alerts: `RawConsumerLag`, `PendingMessagesCritical`, `RedisRawStreamGrowth`
- App OOM / panic / container restart loop

## Diagnosis
```bash
docker logs gps-receiver --tail 200
curl -s http://127.0.0.1:8080/metrics | findstr "gps_raw_ pending worker"
docker exec gps-redis redis-cli XPENDING gps:raw_incoming gps-raw-workers
docker stats gps-receiver --no-stream
```

## Commands
```bash
# Restart with graceful drain (preferred)
docker compose stop -t 90 app
docker compose up -d app

# Inspect PEL
docker exec gps-redis redis-cli XPENDING gps:raw_incoming gps-raw-workers - + 10
```

## Recovery steps
1. Confirm Redis still accepting XADD (ACK path) — fleet safety
2. Fix crash cause (panic stack, OOM → raise memory / reduce WORKER_COUNT temporarily)
3. Restart app; `XAUTOCLAIM` reclaims idle pending entries
4. If destinations down: expect retry stream growth — do not kill raw PEL by trimming
5. Scale `WORKER_COUNT` only after crash stabilized

## Validation
- Pending count trending down over 10–15 minutes
- No continuous restart in `docker ps`
- Decode/DLQ rates not exploding

## Notes
Crash mid-handler leaves messages in PEL — reclaim is by design (Zero-Drop).
