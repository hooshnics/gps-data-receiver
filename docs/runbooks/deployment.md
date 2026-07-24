# Runbook: Deployment

## Symptoms (healthy deploy)
- Brief TCP drain; devices reconnect
- Temporary pending reclaim after restart
- No sustained ingest error spike

## Goal
**Zero packet loss:** ACK only after Redis XADD; during drain, stop listener first so in-flight sessions finish or devices retransmit.

## Diagnosis (pre-flight)
```bash
curl -sf http://127.0.0.1:8080/health
curl -sf http://127.0.0.1:8080/ready
curl -s http://127.0.0.1:8080/metrics | findstr "gps_raw_pending gps_teltonika_ingest"
docker exec gps-redis redis-cli PING
```

## Commands
```bash
# Build + rolling replace with 90s grace (compose stop_grace_period)
docker compose build app
docker compose up -d --no-deps app

# Or explicit drain
docker kill -s SIGTERM gps-receiver
# wait until container exits (up to stop_grace_period)
docker compose up -d app
```

## Recovery steps (ordered shutdown — already in app)
1. SIGTERM
2. Teltonika TCP `Stop()` — close listener, wait sessions
3. Raw / reports / retry consumers stop
4. HTTP `Shutdown` (30s)
5. New container starts; healthcheck must pass before traffic reliance

## Validation
- `/health` and `/ready` OK
- Prometheus `up{job="gps-receiver"}==1`
- ACK counters increase within 1–2 minutes
- Pending not unbounded

## Checklist
See [deployment-checklist.md](./deployment-checklist.md)
