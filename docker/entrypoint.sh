#!/bin/sh
set -e

if [ -z "${LD_PRELOAD:-}" ]; then
    for candidate in /usr/lib/*/libjemalloc.so.2 /usr/lib/libjemalloc.so.2; do
        if [ -f "$candidate" ]; then
            LD_PRELOAD="$candidate"
            export LD_PRELOAD
            break
        fi
    done
fi

exec /usr/local/bin/hips "$@"
