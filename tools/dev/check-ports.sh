#!/bin/sh

set -eu

occupied=0
for port in "$@"; do
  pids=$(lsof -nP -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)
  if [ -z "$pids" ]; then
    continue
  fi

  occupied=1
  echo "development port $port is already in use:"
  for pid in $pids; do
    ps -p "$pid" -o pid=,command= 2>/dev/null || echo "  pid $pid"
  done
done

if [ "$occupied" -ne 0 ]; then
  echo "stop the listed process or choose a different port before running make dev"
  exit 1
fi
