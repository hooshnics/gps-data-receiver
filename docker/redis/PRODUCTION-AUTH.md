# Redis authentication (production)

Lab Compose keeps Redis **passwordless** on the Docker network with **host ports bound to 127.0.0.1 only**.
That is intentional for local Sentinel drills. Public production must add auth.

## App support

`REDIS_PASSWORD` is passed to:

- `redis.Options.Password` (standalone)
- `redis.FailoverOptions.Password` (Sentinel → master)

`ENVIRONMENT=production` requires a non-empty password unless lab escape flags are set.

## Enabling requirepass without breaking Sentinel

1. Choose a strong password; store in secret manager.
2. On **primary** and **replica** conf:

```
requirepass YOUR_SECRET
masterauth YOUR_SECRET
```

3. On **each Sentinel** conf:

```
sentinel auth-pass mymaster YOUR_SECRET
```

4. App / exporters:

```
REDIS_PASSWORD=YOUR_SECRET
```

5. Healthchecks using `redis-cli` must pass `-a` / `REDISCLI_AUTH`.

6. Roll one node at a time; verify `SENTINEL get-master-addr-by-name mymaster` after each step.

## Network rules (non-negotiable)

- Do not publish `6379` / `26379` to `0.0.0.0` on public hosts.
- Prefer private VPC + security groups.
- `protected-mode` alone is not enough if ports are exposed without auth.

## Compose lab note

Default `docker/redis/*.conf` uses `protected-mode no` because containers talk by Compose DNS.
Host publish lines remain `127.0.0.1:6379` / `6380`. Do not change to `0.0.0.0` without auth + firewall.
