#!/usr/bin/env sh
set -eu

# Verifies the Broadcast vertical slice stays live and keeps producing HTTP-FLV.
# Requires: infra up, control plane running, FFmpeg installed, H.264/AAC source.
#
#   DURATION=10 scripts/soak-broadcast.sh /tmp/fixture.mp4   # quick smoke
#   scripts/soak-broadcast.sh /tmp/fixture.mp4               # 10-minute soak

API="${CONTROL_PLANE_URL:-http://127.0.0.1:8081}"
SOURCE="${1:-/tmp/fixture.mp4}"
DURATION="${DURATION:-600}"
INTERVAL="${INTERVAL:-5}"

json_field() {
  node -e 'const body = JSON.parse(require("fs").readFileSync(0, "utf8")); process.stdout.write(String(body[process.argv[1]] ?? ""))' "$1"
}

created=$(curl -sf -X POST "$API/api/v1/broadcasts" \
  -H 'content-type: application/json' \
  -d "{\"source\":{\"type\":\"file\",\"path\":\"$SOURCE\",\"loop\":true},\"latencyClass\":\"low\"}")

id=$(printf '%s' "$created" | json_field broadcastId)
playback=$(printf '%s' "$created" | node -e 'const body = JSON.parse(require("fs").readFileSync(0, "utf8")); process.stdout.write(body.playback.url)')

echo "broadcast $id"
echo "playback $playback"

curl -sf -X POST "$API/api/v1/broadcasts/$id/start" >/dev/null
echo "live; checking every ${INTERVAL}s for ${DURATION}s"

deadline=$(( $(date +%s) + DURATION ))
checks=0
while [ "$(date +%s)" -lt "$deadline" ]; do
  status=$(curl -sf "$API/api/v1/broadcasts/$id" | json_field status)
  if [ "$status" != "live" ]; then
    echo "FAIL: status is $status after $checks checks"
    exit 1
  fi

  header=$(curl -s --max-time 3 "$playback" 2>/dev/null | head -c 3)
  if [ "$header" != "FLV" ]; then
    echo "FAIL: no HTTP-FLV data after $checks checks"
    exit 1
  fi

  checks=$((checks + 1))
  sleep "$INTERVAL"
done

curl -sf -X POST "$API/api/v1/broadcasts/$id/stop" >/dev/null
echo "PASS: $checks checks, stopped $id"
