# Archive strategy (implemented)

## Purpose

Persist raw Teltonika frames after Redis durability for long-term recovery, without blocking TCP ACK.

## Path

```
Device → TCP → CRC → Redis XADD → ACK
                      │
                      ▼
                 Raw consumers
                      │
                      ▼
            Archive Writer (optional)
                      │
                      ▼
            ARCHIVE_DIR/YYYY/MM/DD/raw-YYYYMMDD.ndjson.gz
```

## Enable

```bash
ARCHIVE_ENABLED=true
ARCHIVE_DIR=/var/lib/gps-archive
ARCHIVE_STRICT=false   # true = leave pending if archive I/O fails
```

Compose lab mounts volume `archive-data` → `/var/lib/gps-archive`.

### Volume permissions (required)

The app runs as **`appuser` (uid/gid 1000)** after a short root entrypoint.

Named Docker volumes are often created as **root:root**. Without a fix you get:

```text
mkdir /var/lib/gps-archive/2026: permission denied
```

**Fix (already in image):** `docker/entrypoint.sh` starts as root, `chown 1000:1000` on `ARCHIVE_DIR`, then `su-exec appuser` to run `./gps-receiver`.

Startup also calls `archive.EnsureWritable` — if the directory is not writable the process **exits immediately** with a clear error (fail-fast).

**Do not** set `user: "1000:1000"` on the app service unless the volume is already owned by uid 1000 (entrypoint cannot chown without root).

**Manual repair (existing broken volume):**

```bash
docker compose run --rm --user root --entrypoint sh app -c \
  "chown -R 1000:1000 /var/lib/gps-archive && chmod 0750 /var/lib/gps-archive"
docker compose up -d --build app
```

**Production checklist**

- [ ] `ARCHIVE_DIR` on a dedicated volume
- [ ] Volume owned by uid/gid `1000` (or entrypoint allowed to start as root briefly)
- [ ] App logs: `Raw archive writer ready`
- [ ] Path exists: `/var/lib/gps-archive/YYYY/MM/DD/`
- [ ] Disk alert (`HostDiskUsageHigh`) wired

## Record shape (NDJSON)

Each line is JSON compatible with `cmd/replay` file source:

```json
{"id":"1710000000000-0","imei":"3568…","frame_hex":"…","received_at":"…","source":"tcp","archived_at":"…"}
```

## Daily rotation

On UTC day change the writer closes the previous gzip and opens `raw-YYYYMMDD.ndjson.gz`.

## Recovery usage

```bash
gunzip -c /var/lib/gps-archive/2026/07/23/raw-20260723.ndjson.gz > /tmp/day.ndjson
go run ./cmd/replay -source file -file /tmp/day.ndjson -mode dry-run
# enqueue to non-live stream only:
go run ./cmd/replay -source file -file /tmp/day.ndjson -mode enqueue -target-stream gps:raw_replay
```

## Ops

1. Monitor disk (`HostDiskUsageHigh`).
2. Nightly sync gzip files to object storage; delete local after verify.
3. Prefer `ARCHIVE_STRICT=false` unless compliance requires archive-before-ACK-of-stream-entry.

## Related

- ADR 0023
- [archival.md](./archival.md) (retention / cost notes)
- [replay.md](./replay.md)
