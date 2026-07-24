#!/bin/sh
set -eu

# PostgreSQL data directories are not forward-compatible across major
# versions. Keep the old and new binaries in the image and perform the one
# supported transition for this image before PostgreSQL or Heya starts.
#
# pg_upgrade --link keeps the migration fast and avoids needing another full
# copy of large vector indexes. It also means the old cluster is not a backup:
# callers must take a real backup before deploying an image with a new major.

: "${HEYA_POSTGRES_TARGET_MAJOR:=18}"
case "$HEYA_POSTGRES_TARGET_MAJOR" in
    ''|*[!0-9]*)
        echo "HEYA_POSTGRES_TARGET_MAJOR must be an integer" >&2
        exit 1
        ;;
esac
target_major=$HEYA_POSTGRES_TARGET_MAJOR
source_major=$((target_major - 1))
old_port=50431
new_port=50432

: "${PGDATA:?PGDATA must be set}"
: "${POSTGRES_USER:=postgres}"

case "$PGDATA" in
    /)
        echo "PGDATA may not be the filesystem root" >&2
        exit 1
        ;;
    /*/)
        PGDATA=${PGDATA%/}
        ;;
    /*)
        ;;
    *)
        echo "PGDATA must be an absolute path" >&2
        exit 1
        ;;
esac
export PGDATA POSTGRES_USER

data_parent=$(dirname "$PGDATA")
data_name=$(basename "$PGDATA")
old_data="${data_parent}/${data_name}.pg${source_major}-upgrade"
new_data="${data_parent}/${data_name}.pg${target_major}-upgrade"
work_dir="${data_parent}/.${data_name}.pg-upgrade-work"
socket_dir="${work_dir}/socket"
upgrade_marker=".heya-postgres-upgrade-${source_major}-to-${target_major}"

old_bin="/usr/lib/postgresql/${source_major}/bin"
new_bin="/usr/lib/postgresql/${target_major}/bin"

old_server_running=no
new_server_running=no
target_activated=no
upgrade_started=no

run_as_postgres() {
    if [ "$(id -u)" -eq 0 ]; then
        runuser -u postgres -- "$@"
    else
        "$@"
    fi
}

stop_old_server() {
    if [ "$old_server_running" = yes ]; then
        run_as_postgres "$old_bin/pg_ctl" \
            --pgdata="$PGDATA" --mode=fast --wait stop >/dev/null 2>&1 || true
        old_server_running=no
    fi
}

stop_new_server() {
    if [ "$new_server_running" = yes ]; then
        run_as_postgres "$new_bin/pg_ctl" \
            --pgdata="$PGDATA" --mode=fast --wait stop >/dev/null 2>&1 || true
        new_server_running=no
    fi
}

restore_source_control_file() {
    cluster_dir=$1
    if [ ! -e "$cluster_dir/global/pg_control" ] && [ -e "$cluster_dir/global/pg_control.old" ]; then
        # pg_upgrade renames the source control file before link mode finishes.
        # Until Heya activates and starts the target cluster, restoring that
        # name is safe and makes an interrupted source cluster bootable again.
        mv "$cluster_dir/global/pg_control.old" "$cluster_dir/global/pg_control"
    fi
}

cleanup_on_exit() {
    status=$?
    trap - EXIT HUP INT TERM

    stop_new_server
    stop_old_server

    if [ "$status" -ne 0 ] && [ "$target_activated" = no ] && [ "$upgrade_started" = yes ]; then
        # Before activation the source cluster is still authoritative. Put it
        # back at PGDATA so a fixed image can retry the migration.
        rm -rf "$new_data" "$work_dir"
        if [ ! -e "$PGDATA" ] && [ -d "$old_data" ]; then
            echo "PostgreSQL upgrade failed before activation; restoring PostgreSQL $source_major data"
            restore_source_control_file "$old_data"
            mv "$old_data" "$PGDATA"
        fi
        if [ -d "$PGDATA" ]; then
            rm -f "$PGDATA/$upgrade_marker"
        fi
    fi

    exit "$status"
}

trap cleanup_on_exit EXIT
trap 'exit 130' HUP INT TERM

prepare_work_dir() {
    rm -rf "$work_dir"
    install -d -m 0700 -o postgres -g postgres "$work_dir" "$socket_dir"
}

remove_stale_postmaster_pid() {
    cluster_dir=$1
    cluster_bin=$2

    if [ ! -f "$cluster_dir/postmaster.pid" ]; then
        return
    fi

    if run_as_postgres "$cluster_bin/pg_ctl" --pgdata="$cluster_dir" status >/dev/null 2>&1; then
        echo "PostgreSQL is already running for $cluster_dir; refusing to upgrade it" >&2
        exit 1
    fi

    echo "Removing stale postmaster.pid from $cluster_dir"
    rm -f "$cluster_dir/postmaster.pid"
}

verify_target_cluster() {
    install -d -m 0700 -o postgres -g postgres "$work_dir"
    rm -rf "$socket_dir"
    install -d -m 0700 -o postgres -g postgres "$socket_dir"
    remove_stale_postmaster_pid "$PGDATA" "$new_bin"

    target_options="-c listen_addresses='' -c unix_socket_directories='$socket_dir' -p $new_port"
    run_as_postgres "$new_bin/pg_ctl" \
        --pgdata="$PGDATA" --options="$target_options" --wait start
    new_server_running=yes

    # LOAD exercises the target pgvector shared library even when the
    # cluster predates Heya's vector extension migration.
    run_as_postgres "$new_bin/psql" \
        --host="$socket_dir" \
        --port="$new_port" \
        --username="$POSTGRES_USER" \
        --dbname=postgres \
        --no-password \
        --no-psqlrc \
        --set=ON_ERROR_STOP=1 \
        --command="LOAD 'vector'; SELECT current_setting('server_version_num')::int / 10000 = $target_major AS target_postgres"

    if [ -s "$work_dir/update_extensions.sql" ]; then
        echo "Applying extension updates recommended by pg_upgrade"
        run_as_postgres "$new_bin/psql" \
            --host="$socket_dir" \
            --port="$new_port" \
            --username="$POSTGRES_USER" \
            --dbname=postgres \
            --no-password \
            --no-psqlrc \
            --set=ON_ERROR_STOP=1 \
            --file="$work_dir/update_extensions.sql"
    fi

    # Modern PostgreSQL carries most optimizer statistics through pg_upgrade.
    # Fill gaps (for extension-defined statistics, for example) before
    # readiness so the first Heya queries cannot inherit an avoidable bad plan.
    analyze_jobs=$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 1)
    case "$analyze_jobs" in
        ''|*[!0-9]*)
            analyze_jobs=1
            ;;
    esac
    echo "Analyzing relations with missing optimizer statistics"
    run_as_postgres "$new_bin/vacuumdb" \
        --host="$socket_dir" \
        --port="$new_port" \
        --username="$POSTGRES_USER" \
        --all \
        --analyze-in-stages \
        --missing-stats-only \
        --jobs="$analyze_jobs" \
        --no-password

    stop_new_server
}

# Recover the only safe interrupted pre-activation state. Once target PostgreSQL
# has been placed at PGDATA we never fall back to the linked source cluster;
# starting the new cluster makes those old files unsafe to reuse.
if [ ! -s "$PGDATA/PG_VERSION" ] && [ -s "$old_data/PG_VERSION" ]; then
    if [ ! -f "$old_data/$upgrade_marker" ]; then
        echo "Refusing to recover unmarked PostgreSQL data from $old_data" >&2
        exit 1
    fi
    echo "Recovering an interrupted PostgreSQL $source_major to $target_major upgrade before activation"
    rm -rf "$new_data" "$work_dir" "$PGDATA"
    restore_source_control_file "$old_data"
    mv "$old_data" "$PGDATA"
    rm -f "$PGDATA/$upgrade_marker"
fi

if [ ! -s "$PGDATA/PG_VERSION" ]; then
    # Fresh volume. The normal image entrypoint will initialize target PostgreSQL.
    exit 0
fi

current_major=$(cat "$PGDATA/PG_VERSION")

if [ "$current_major" = "$target_major" ]; then
    if [ -d "$old_data" ]; then
        if [ ! -f "$old_data/$upgrade_marker" ]; then
            echo "Refusing to remove unmarked PostgreSQL data from $old_data" >&2
            exit 1
        fi
        echo "Finishing verification of an interrupted PostgreSQL $target_major activation"
        target_activated=yes
        verify_target_cluster
        rm -rf "$old_data" "$new_data" "$work_dir"
    fi
    exit 0
fi

if [ "$current_major" != "$source_major" ]; then
    echo "PostgreSQL data version $current_major cannot be upgraded by this image; expected $source_major or $target_major" >&2
    exit 1
fi

for required_binary in \
    "$old_bin/pg_ctl" \
    "$old_bin/pg_controldata" \
    "$new_bin/initdb" \
    "$new_bin/pg_upgrade" \
    "$new_bin/pg_ctl" \
    "$new_bin/psql" \
    "$new_bin/vacuumdb"
do
    if [ ! -x "$required_binary" ]; then
        echo "Required PostgreSQL upgrade binary is missing: $required_binary" >&2
        exit 1
    fi
done

if [ -e "$old_data" ] || [ -e "$new_data" ]; then
    echo "Unexpected PostgreSQL upgrade directory already exists beside PGDATA" >&2
    echo "Refusing to remove it automatically: $old_data or $new_data" >&2
    exit 1
fi

echo "Inspecting PostgreSQL $source_major cluster settings before upgrade"
upgrade_started=yes
prepare_work_dir
remove_stale_postmaster_pid "$PGDATA" "$old_bin"

old_options="-c listen_addresses='' -c unix_socket_directories='$socket_dir' -p $old_port"
run_as_postgres "$old_bin/pg_ctl" \
    --pgdata="$PGDATA" --options="$old_options" --wait start
old_server_running=yes

cluster_settings=$(run_as_postgres "$old_bin/psql" \
    --host="$socket_dir" \
    --port="$old_port" \
    --username="$POSTGRES_USER" \
    --dbname=postgres \
    --no-password \
    --no-psqlrc \
    --tuples-only \
    --no-align \
    --field-separator='|' \
    --set=ON_ERROR_STOP=1 \
    --command="SELECT pg_encoding_to_char(encoding), datcollate, datctype, datlocprovider, COALESCE(datlocale, '') FROM pg_database WHERE datname = 'template0'")

stop_old_server

old_ifs=$IFS
IFS='|'
set -f
# Intentional pipe-delimited field splitting with pathname expansion disabled.
# shellcheck disable=SC2086
set -- $cluster_settings
set +f
IFS=$old_ifs

encoding=${1:-}
collate=${2:-}
ctype=${3:-}
locale_provider=${4:-}
provider_locale=${5:-}

if [ -z "$encoding" ] || [ -z "$collate" ] || [ -z "$ctype" ] || [ -z "$locale_provider" ]; then
    echo "Could not determine the PostgreSQL $source_major cluster locale and encoding" >&2
    exit 1
fi

case "$locale_provider" in
    c)
        ;;
    i|b)
        if [ -z "$provider_locale" ]; then
            echo "PostgreSQL locale provider $locale_provider did not report a locale" >&2
            exit 1
        fi
        ;;
    *)
        echo "Unsupported PostgreSQL locale provider: $locale_provider" >&2
        exit 1
        ;;
esac

checksum_version=$(run_as_postgres "$old_bin/pg_controldata" "$PGDATA" |
    awk -F: '/Data page checksum version/ { gsub(/[[:space:]]/, "", $2); print $2 }')
case "$checksum_version" in
    0)
        checksum_option=--no-data-checksums
        ;;
    ''|*[!0-9]*)
        echo "Could not determine the PostgreSQL $source_major data checksum setting" >&2
        exit 1
        ;;
    *)
        checksum_option=--data-checksums
        ;;
esac

echo "Upgrading PostgreSQL $source_major data at $PGDATA to PostgreSQL $target_major"
run_as_postgres touch "$PGDATA/$upgrade_marker"
mv "$PGDATA" "$old_data"
install -d -m 0700 -o postgres -g postgres "$new_data"

case "$locale_provider" in
    c)
        run_as_postgres "$new_bin/initdb" \
            --pgdata="$new_data" \
            --username="$POSTGRES_USER" \
            --encoding="$encoding" \
            --locale-provider=libc \
            --lc-collate="$collate" \
            --lc-ctype="$ctype" \
            --auth-local=trust \
            --auth-host=scram-sha-256 \
            "$checksum_option"
        ;;
    i)
        run_as_postgres "$new_bin/initdb" \
            --pgdata="$new_data" \
            --username="$POSTGRES_USER" \
            --encoding="$encoding" \
            --locale-provider=icu \
            --icu-locale="$provider_locale" \
            --auth-local=trust \
            --auth-host=scram-sha-256 \
            "$checksum_option"
        ;;
    b)
        run_as_postgres "$new_bin/initdb" \
            --pgdata="$new_data" \
            --username="$POSTGRES_USER" \
            --encoding="$encoding" \
            --locale-provider=builtin \
            --builtin-locale="$provider_locale" \
            --auth-local=trust \
            --auth-host=scram-sha-256 \
            "$checksum_option"
        ;;
esac

upgrade_jobs=$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 1)
case "$upgrade_jobs" in
    ''|*[!0-9]*)
        upgrade_jobs=1
        ;;
esac

(
    cd "$work_dir"
    run_as_postgres "$new_bin/pg_upgrade" \
        --check \
        --link \
        --jobs="$upgrade_jobs" \
        --username="$POSTGRES_USER" \
        --old-datadir="$old_data" \
        --new-datadir="$new_data" \
        --old-bindir="$old_bin" \
        --new-bindir="$new_bin" \
        --socketdir="$socket_dir" \
        --old-port="$old_port" \
        --new-port="$new_port"

    run_as_postgres "$new_bin/pg_upgrade" \
        --link \
        --jobs="$upgrade_jobs" \
        --username="$POSTGRES_USER" \
        --old-datadir="$old_data" \
        --new-datadir="$new_data" \
        --old-bindir="$old_bin" \
        --new-bindir="$new_bin" \
        --socketdir="$socket_dir" \
        --old-port="$old_port" \
        --new-port="$new_port"
)

# pg_upgrade intentionally leaves configuration behind. Preserve access rules,
# but use target PostgreSQL's generated postgresql.conf so removed settings cannot
# prevent the new server from starting.
for config_file in pg_hba.conf pg_ident.conf
do
    if [ -f "$old_data/$config_file" ]; then
        run_as_postgres cp "$old_data/$config_file" "$new_data/$config_file"
    fi
done

mv "$new_data" "$PGDATA"
target_activated=yes

verify_target_cluster

# The linked source cluster is not a rollback after target PostgreSQL has
# started. Delete it only after the target server and pgvector library pass the
# smoke check.
rm -rf "$old_data" "$new_data" "$work_dir"

echo "PostgreSQL $source_major to $target_major automatic upgrade completed successfully"
