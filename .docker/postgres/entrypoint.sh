#!/bin/sh
set -eu

case "${1:-}" in
    postgres)
        case "${2:-}" in
            --help|-\?|--version|-V)
                ;;
            *)
                /usr/local/bin/heya-postgres-upgrade
                ;;
        esac
        ;;
esac

exec /usr/local/bin/docker-entrypoint.sh "$@"
