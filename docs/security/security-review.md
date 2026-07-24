# Security review — GPS Data Receiver (Phase 2)

Lightweight audit focused on real exposure. No unnecessary control layers added.

## Scope

TCP `:5055`, HTTP `:8080`, Redis, PostgreSQL, secrets, logging, compose defaults.

## Findings

| Area | Current state | Risk | Action |
|------|---------------|------|--------|
| TCP port exposure | Compose publishes `5055:5055` (all interfaces) | High if host is public without firewall | **Ops:** firewall / security group allowlist (device APNs / VPN). Documented. |
| IMEI authentication | Production requires `TELTONIKA_IMEI_WHITELIST`; lab may use `GPS_LAB_MODE` + `TELTONIKA_ALLOW_ANY_IMEI` | Medium if misconfigured | **Hardened:** production startup fails without whitelist (unless lab escape). |
| Redis exposure | Bound to `127.0.0.1:6379` / `6380` in compose; password empty in lab; `protected-mode no` inside Docker network | High **if** port published publicly | Keep localhost bind. Production requires `REDIS_PASSWORD`. See `docker/redis/PRODUCTION-AUTH.md`. |
| Database credentials | Lab `gps`/`gps`; production rejects defaults | High if unchanged in prod | **Hardened:** production fails on default/empty Postgres password. |
| Secrets management | `.env` / compose `environment` | Medium | Use `env.production.example` + [secret-management-checklist.md](../runbooks/secret-management-checklist.md). |

## Fixes applied in Phase 2 / final hardening

1. Redacted full payload from HTTP parse failure logs (`payload_bytes` instead of `raw_data`).
2. Production **hard fail** (not only warn) for empty Redis password, empty IMEI whitelist (TCP on), default Postgres password — with explicit lab escape flags.
3. Compose lab uses `ENVIRONMENT=development` + `GPS_LAB_MODE` (not fake production).
4. Alertmanager wired in compose; Redis/Postgres host ports remain localhost-bound.
5. PiStat HTTP sender + Hooshnics forwarder: timeouts, retries, exponential backoff, circuit-open parking (never on ACK path).

## Explicit non-goals (this phase)

- Device mTLS / Teltonika TLS (fleet-dependent)
- Redis ACL redesign
- Application-level encryption of AVL at rest (rely on volume/object encryption)

## Verification checklist

- [ ] `:6379` / `:5432` not reachable from untrusted networks
- [ ] `:5055` restricted to device egress paths
- [ ] `TELTONIKA_IMEI_WHITELIST` set in production
- [ ] `REDIS_PASSWORD` / Postgres credentials rotated
- [ ] Log sinks do not retain historical full payloads
- [ ] Grafana/Prometheus not world-writable without auth

## References

- ADR 0015, 0017, 0023
- `docker-compose.yml`
- `env.example` / `env.production.example`
- `docker/redis/PRODUCTION-AUTH.md`
- `cmd/server/main.go`
- `docs/runbooks/secret-management-checklist.md`
