#!/bin/sh
set -eu

if [ "${1:-}" != "all-in-one" ]; then
    exec /usr/local/bin/heya "$@"
fi

: "${PGDATA:=/data/postgres}"
: "${POSTGRES_USER:=heya}"
: "${POSTGRES_PASSWORD:=heya}"
: "${POSTGRES_DB:=$POSTGRES_USER}"

export PGDATA POSTGRES_USER POSTGRES_PASSWORD POSTGRES_DB

case "$POSTGRES_USER$POSTGRES_DB" in
    *[!a-zA-Z0-9_-]*)
        echo "POSTGRES_USER and POSTGRES_DB may contain only letters, digits, underscores, and hyphens" >&2
        exit 1
        ;;
esac

case "$POSTGRES_PASSWORD" in
    *"
"*)
        echo "POSTGRES_PASSWORD must not contain a newline" >&2
        exit 1
        ;;
esac

install -d -m 0755 -o postgres -g postgres /run/postgresql
install -d -m 0700 -o postgres -g postgres "$PGDATA"

if [ ! -s "$PGDATA/PG_VERSION" ]; then
    password_file="$(mktemp)"
    trap 'rm -f "$password_file"' EXIT HUP INT TERM
    printf '%s\n' "$POSTGRES_PASSWORD" > "$password_file"
    chown postgres:postgres "$password_file"
    chmod 0600 "$password_file"

    runuser -u postgres -- /usr/lib/postgresql/17/bin/initdb \
        --pgdata="$PGDATA" \
        --username="$POSTGRES_USER" \
        --pwfile="$password_file" \
        --encoding=UTF8 \
        --locale=C.UTF-8 \
        --auth-local=trust \
        --auth-host=scram-sha-256

    # The database is private to this container. Keeping it on loopback also
    # prevents an accidental `-p 5432:5432` from exposing the bundled server.
    cat >> "$PGDATA/postgresql.conf" <<'EOF'
listen_addresses = '127.0.0.1'
unix_socket_directories = '/run/postgresql'
EOF

    rm -f "$password_file"
    trap - EXIT HUP INT TERM
fi

# Query diagnostics need the collector loaded when PostgreSQL starts. Keep
# this outside first-boot initialization so existing AIO data volumes gain the
# setting on their next container restart too.
if ! grep -Eq '^[[:space:]]*shared_preload_libraries[[:space:]]*=.*pg_stat_statements' "$PGDATA/postgresql.conf"; then
    cat >> "$PGDATA/postgresql.conf" <<'EOF'
shared_preload_libraries = 'pg_stat_statements'
pg_stat_statements.track = 'all'
EOF
fi

# Finish database creation separately from initdb so an interrupted first boot
# can safely resume instead of leaving a valid cluster without POSTGRES_DB.
if [ ! -f "$PGDATA/.heya-aio-initialized" ]; then
    runuser -u postgres -- /usr/lib/postgresql/17/bin/pg_ctl \
        --pgdata="$PGDATA" \
        --options="-c listen_addresses='' -c unix_socket_directories=/run/postgresql" \
        --wait start

    database_exists="$(runuser -u postgres -- /usr/bin/psql \
        --host=/run/postgresql \
        --username="$POSTGRES_USER" \
        --dbname=postgres \
        --tuples-only \
        --no-align \
        --command="SELECT 1 FROM pg_database WHERE datname = '$POSTGRES_DB'")"
    if [ "$database_exists" != "1" ]; then
        runuser -u postgres -- /usr/bin/createdb \
            --host=/run/postgresql \
            --username="$POSTGRES_USER" \
            -- "$POSTGRES_DB"
    fi

    runuser -u postgres -- /usr/lib/postgresql/17/bin/pg_ctl \
        --pgdata="$PGDATA" \
        --mode=fast \
        --wait stop

    runuser -u postgres -- touch "$PGDATA/.heya-aio-initialized"
fi

# The defaults intentionally make the zero-configuration `docker run` work.
# If POSTGRES_* is customized, provide a matching HEYA_DATABASE_URL as well.
: "${HEYA_DATABASE_URL:=postgres://heya:heya@127.0.0.1:5432/heya?sslmode=disable}"
export HEYA_DATABASE_URL

exec /usr/bin/supervisord --configuration=/etc/supervisor/conf.d/heya-aio.conf
