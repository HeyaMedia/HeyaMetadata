#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
expected="$root/.dev/air/heya-metadata worker"

while IFS= read -r pid; do
  [[ -n "$pid" ]] || continue
  command="$(ps -p "$pid" -o command= 2>/dev/null || true)"
  parent="$(ps -p "$pid" -o ppid= 2>/dev/null | tr -d ' ' || true)"
  if [[ "$command" != "$expected" || "$parent" != "1" ]]; then
    continue
  fi
  echo "Stopping orphaned HeyaMetadata dev worker (PID $pid)"
  kill -TERM "$pid" 2>/dev/null || true
  for _ in {1..20}; do
    kill -0 "$pid" 2>/dev/null || break
    sleep 0.1
  done
  kill -KILL "$pid" 2>/dev/null || true
done < <(pgrep -f "heya-metadata worker" || true)
