# Runbook: PostgreSQL failure

## Symptoms
- Async storage lag / errors in logs
- Alert `DatabaseConnectionExhaustion` or `PostgresDown`
- Maps/history stale while live ingest may still ACK (Postgres is **after** Redis)

## Diagnosis
```bash
docker ps --filter name=gps-postgres
docker exec gps-postgres pg_isready -U gps -d gps_receiver
docker logs gps-postgres --tail 100
curl -s http://127.0.0.1:8080/metrics | findstr gps_postgres_
```

## Commands
```bash
docker compose up -d postgres
docker exec -it gps-postgres psql -U gps -d gps_receiver -c "SELECT count(*) FROM gps_records;"
docker exec gps-postgres psql -U gps -d gps_receiver -c "SELECT state, count(*) FROM pg_stat_activity GROUP BY 1;"
```

## Recovery steps
1. Restore Postgres container / disk / credentials
2. If pool exhausted: restart app **gracefully** (SIGTERM) after Postgres healthy, or raise `WORKER_COUNT` carefully (opens more DB conns)
3. Confirm `ON CONFLICT DO NOTHING` dedupe absorbs retransmission bursts
4. PiStat retry streams continue independently — clear destination issues separately

## Validation
- `pg_isready` OK
- `gps_postgres_in_use_connections / max < 0.9`
- New rows appearing in `gps_records` after successful deliveries
- `/ready` healthy if readiness includes DB

## Notes
Postgres outage must **not** block TCP ACK (ACK depends only on Redis XADD). Prioritize Redis health first.
