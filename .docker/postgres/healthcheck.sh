#!/bin/sh
set -eu

: "${PGDATA:?PGDATA must be set}"
: "${HEYA_POSTGRES_TARGET_MAJOR:=18}"

if [ ! -s "$PGDATA/PG_VERSION" ] || [ "$(cat "$PGDATA/PG_VERSION")" != "$HEYA_POSTGRES_TARGET_MAJOR" ]; then
    exit 1
fi

exec pg_isready --username="${POSTGRES_USER:-postgres}" --dbname="${POSTGRES_DB:-${POSTGRES_USER:-postgres}}"
