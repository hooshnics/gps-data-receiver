# Runbook: Rollback

## Symptoms
- Post-deploy spike in CRC/decode/ingest errors
- Health failing repeatedly
- ACK latency regression vs baseline

## Diagnosis
```bash
docker images gps-receiver
docker logs gps-receiver --tail 200
curl -s http://127.0.0.1:8080/metrics | findstr "gps_teltonika_ ingest decode"
```

## Commands
```bash
# Tag previous known-good before upgrade (habit)
docker tag gps-receiver:latest gps-receiver:previous

# Rollback image
docker tag gps-receiver:previous gps-receiver:latest
docker compose up -d --no-deps app

# Or rebuild from last good git commit
git checkout <good-sha> -- .
docker compose build app
docker compose up -d --no-deps app
```

## Recovery steps
1. Roll back **app** first (Redis/Postgres usually stay)
2. Use SIGTERM / `stop_grace_period` — avoid `kill -9` unless hung
3. If parser regression already ACKed bad logic into consumers: use [data-recovery.md](./data-recovery.md) + `cmd/replay`
4. Do not change Zero-Drop ACK ordering as a “fix”

## Validation
- Error rates return to pre-deploy baseline
- Load simulator smoke: `go run ./cmd/teltonika-simulator -devices 50 -duration 1m`
- Alerts clear in Prometheus

## Notes
Redis stream data remains durable across app rollbacks. Devices cover un-ACKed windows.
