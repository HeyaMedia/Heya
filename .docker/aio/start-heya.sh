#!/bin/sh
set -eu

until /usr/bin/pg_isready \
    --host=127.0.0.1 \
    --port=5432 \
    --username="${POSTGRES_USER:-heya}" \
    --dbname="${POSTGRES_DB:-heya}" >/dev/null 2>&1; do
    sleep 1
done

exec /usr/local/bin/heya serve
