# Secret management checklist

Use before production cutover and on every credential rotation.

## Inventory

- [ ] `POSTGRES_PASSWORD` (non-default; not `gps` / `password` / `postgres`)
- [ ] `REDIS_PASSWORD` (required when `ENVIRONMENT=production`)
- [ ] `HOOSHNICS_AUTH_TOKEN` / backend `GPS_INGEST_TOKEN` (if mirror enabled)
- [ ] Grafana admin password
- [ ] Alertmanager webhook / SMTP credentials
- [ ] Object-storage keys for cold archive sync

## Storage

- [ ] Secrets live in orchestrator secret store / sealed secrets / vault — not git
- [ ] `.env` on hosts is mode `600` and excluded from backups that are widely readable
- [ ] `env.example` / `env.production.example` contain placeholders only

## Redis auth (production)

Lab Compose uses empty password + localhost bind. For production:

1. Set `requirepass` / `masterauth` on primary and replica (same password).
2. Set `sentinel auth-pass mymaster <password>` on all Sentinels.
3. Set app `REDIS_PASSWORD` (go-redis FailoverClient uses it for master).
4. Do **not** publish Redis/Sentinel ports to the public internet.
5. See `docker/redis/PRODUCTION-AUTH.md`.

## Validation

- [ ] App starts with `ENVIRONMENT=production` without `GPS_LAB_MODE`
- [ ] Intentional misconfig (empty Redis password) fails fast at startup
- [ ] Rotation procedure documented; dual-write window if needed

## Never

- [ ] Commit real `.env`
- [ ] Use lab allow flags (`TELTONIKA_ALLOW_ANY_IMEI`, `POSTGRES_ALLOW_DEFAULT_PASSWORD`, `REDIS_ALLOW_EMPTY_PASSWORD`) outside isolated labs
