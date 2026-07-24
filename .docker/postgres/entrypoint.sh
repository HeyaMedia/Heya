#!/bin/sh
set -eu

run_upgrade=no
case "${1:-}" in
    postgres|-*)
        run_upgrade=yes
        ;;
esac

# Match the upstream entrypoint's help/version behavior. Kubernetes commonly
# supplies only `-c ...` arguments; docker-entrypoint.sh later prepends
# `postgres`, so those invocations must run the upgrade too.
for arg in "$@"
do
    case "$arg" in
        --help|-\?|--describe-config|--version|-V)
            run_upgrade=no
            break
            ;;
    esac
done

if [ "$run_upgrade" = yes ]; then
    /usr/local/bin/heya-postgres-upgrade
fi

exec /usr/local/bin/docker-entrypoint.sh "$@"
