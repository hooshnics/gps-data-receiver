#!/bin/sh
# Entrypoint: fix archive volume ownership, then drop to appuser (uid 1000).
# Named Docker volumes are often created as root; the process must not run as root.
set -eu

APP_USER="${APP_USER:-appuser}"
APP_UID="${APP_UID:-1000}"
APP_GID="${APP_GID:-1000}"
ARCHIVE_DIR="${ARCHIVE_DIR:-/var/lib/gps-archive}"

fix_archive_perms() {
  if [ "$(id -u)" -ne 0 ]; then
    return 0
  fi
  if [ -z "${ARCHIVE_ENABLED:-}" ] || [ "${ARCHIVE_ENABLED}" = "false" ] || [ "${ARCHIVE_ENABLED}" = "0" ]; then
    # Still ensure mount point exists and is owned correctly when path is present.
    :
  fi
  mkdir -p "${ARCHIVE_DIR}"
  # Fix mount root and any root-owned tree left by prior broken runs / docker exec.
  chown -R "${APP_UID}:${APP_GID}" "${ARCHIVE_DIR}"
  chmod 0750 "${ARCHIVE_DIR}"
}

if [ "$(id -u)" -eq 0 ]; then
  fix_archive_perms
  exec su-exec "${APP_USER}" "$@"
fi

exec "$@"
