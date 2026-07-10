#!/bin/sh

set -eu

cache_dir=${1:?cache directory is required}
max_mb=${2:?maximum size in MiB is required}

case "$max_mb" in
  *[!0-9]*|'')
    echo "invalid Go cache limit: $max_mb" >&2
    exit 2
    ;;
esac

[ -d "$cache_dir" ] || exit 0

size_kb=$(du -sk "$cache_dir" | awk '{print $1}')
max_kb=$((max_mb * 1024))
if [ "$size_kb" -le "$max_kb" ]; then
  exit 0
fi

echo "Go build cache exceeded ${max_mb} MiB; clearing $cache_dir"
GOCACHE="$cache_dir" go clean -cache
