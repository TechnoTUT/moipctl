#!/bin/bash
# Try the web UI end to end without hardware: fake nodes, a real moipctl with the UI embedded, headless Chrome.
#   make all            # builds the UI and bin/moipctl with it embedded
#   test/ui/run.sh      # or: make ui-check
# Needs python3, node (22+) and google-chrome. Screenshots go to $OUT (default /tmp/moipctl-ui-check).
set -euo pipefail
cd "$(dirname "$0")/../.."
OUT=${OUT:-/tmp/moipctl-ui-check}
PORT=${PORT:-18080}
mkdir -p "$OUT"
[ -x bin/moipctl ] || { echo "bin/moipctl is missing: run 'make all'"; exit 1; }

pids=()
cleanup() { for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done; }
trap cleanup EXIT

python3 test/ui/fakenodes.py > "$OUT/fakenodes.log" 2>&1 & pids+=($!)
sleep 1
nodes=(); for p in 19001 19002 19003 19004 19005 19006; do nodes+=(--node "127.0.0.1:$p"); done # 19006 has no server: an unreachable node
rm -f "$OUT/state.json"
bin/moipctl serve --port "$PORT" --mdns=false --state "$OUT/state.json" "${nodes[@]}" > "$OUT/moipctl.log" 2>&1 & pids+=($!)
for _ in $(seq 40); do curl -sf "localhost:$PORT/healthz" >/dev/null && break; sleep 0.25; done

alias() { curl -sf -X PUT -H 'Content-Type: application/json' -d "{\"alias\":\"$2\"}" "localhost:$PORT/api/nodes/static:127.0.0.1:$1/alias" >/dev/null; }
alias 19001 "Entrance Signage"; alias 19002 "Booth Monitor"; alias 19003 "Stage Left"; alias 19004 "DJ Camera (TX)"; alias 19005 "Stage Right"
sleep 2

node test/ui/check.mjs "http://127.0.0.1:$PORT/" "$OUT"
echo "screenshots: $OUT/*.png"
