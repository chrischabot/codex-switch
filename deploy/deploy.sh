#!/bin/sh
# Deploy a new codex-switch generation without interrupting admitted work.
#
# Mechanism: every generation binds the listen port
# with SO_REUSEPORT and macOS delivers new connections to the most recently
# bound listener. A short-lived bridge process takes new work immediately;
# the supervised old generation drains admitted work and exits; the
# supervisor (one launchd agent, written once at install, never touched
# here) respawns the stable path, which now holds the new binary; the
# bridge then drains its brief admissions. Break-glass: `deploy.sh back`.
#
# Preconditions: a supervisor keeps STABLE_BINARY running; the proxy serves
# GET <capability>/codex-switch/deploy-status returning {revision,dirty,pid}.
set -eu

usage() {
  echo "usage: deploy.sh new <new-binary> --stable <path> --data <dir> --listen <addr>" >&2
  echo "       deploy.sh back --stable <path> --data <dir> --listen <addr>" >&2
  exit 2
}

MODE="${1:-}"; shift || usage
NEW_BINARY=""
case "$MODE" in
  new) NEW_BINARY="$1"; shift ;;
  back) ;;
  *) usage ;;
esac

STABLE=""; DATA=""; LISTEN=""
while [ $# -gt 0 ]; do
  case "$1" in
    --stable) STABLE="$2"; shift 2 ;;
    --data)   DATA="$2";   shift 2 ;;
    --listen) LISTEN="$2"; shift 2 ;;
    *) usage ;;
  esac
done
[ -n "$STABLE" ] && [ -n "$DATA" ] && [ -n "$LISTEN" ] || usage

CAPABILITY="$(cat "$DATA/client-capability")"
STATUS_URL="http://$LISTEN/client/$CAPABILITY/codex-switch/deploy-status"

deploy_status() { printf 'url = "%s"\n' "$STATUS_URL" | curl -sf --max-time 5 --config -; }
status_field() { echo "$1" | /usr/bin/sed -E "s/.*\"$2\":\"?([^,\"}]*)\"?.*/\1/"; }

OLD_STATUS="$(deploy_status)" || { echo "no serving generation answered on $LISTEN" >&2; exit 1; }
OLD_PID="$(status_field "$OLD_STATUS" pid)"

# Binary replacement must swap the inode (cp-then-mv): overwriting a signed
# macOS executable in place invalidates its signature and the kernel kills
# anything exec'd from it. mv also leaves running processes on their inode.
replace_stable() {
  cp "$1" "$STABLE.next"
  mv -f "$STABLE.next" "$STABLE"
}
if [ "$MODE" = new ]; then
  TARGET_BINARY="$NEW_BINARY"
  DETACH_BINARY="$NEW_BINARY"
else
  [ -f "$STABLE.previous" ] || { echo "no previous binary at $STABLE.previous" >&2; exit 1; }
  TARGET_BINARY="$STABLE.previous"
  DETACH_BINARY="$STABLE"
fi
TARGET_REVISION="$(CODEX_SWITCH_DATA_DIR=$DATA "$TARGET_BINARY" print-revision)" || { echo "candidate could not report its revision; stable binary unchanged" >&2; exit 1; }
case "$TARGET_REVISION" in
  ''|unknown) echo "candidate has no source revision; build it with scripts/dev/build-proxy" >&2; exit 1 ;;
esac
# The bundled binary starts a separate session using only macOS system APIs.
# It opens its own log so capturing the PID cannot wait for the bridge to exit.
BRIDGE_PID=$(CODEX_SWITCH_DATA_DIR="$DATA" "$DETACH_BINARY" start-detached "$DATA/deployment-bridge.log" "$TARGET_BINARY" serve --listen "$LISTEN" --check-providers)
bridge_alive() {
  case "$(/bin/ps -p "$BRIDGE_PID" -o stat= | tr -d ' ')" in
    ''|Z*) return 1 ;;
    *) kill -0 "$BRIDGE_PID" 2>/dev/null ;;
  esac
}

# The bridge must demonstrably own new connections before the old
# generation is signalled (closing a listener resets its queued backlog).
# The candidate checks every signed-in account with a fresh provider read
# before it listens. Failed reads exit without taking traffic.
BRIDGE_READY=false
for _ in $(seq 1 300); do
  if ! bridge_alive; then
    echo "candidate exited before serving; stable binary unchanged; old generation keeps serving" >&2
    tail -n 1 "$DATA/deployment-bridge.log" >&2
    exit 1
  fi
  STATUS="$(deploy_status || true)"
  if [ "$(status_field "$STATUS" pid)" = "$BRIDGE_PID" ] &&
    [ "$(status_field "$STATUS" revision)" = "$TARGET_REVISION" ]; then
    BRIDGE_READY=true
    break
  fi
  sleep 0.2
done
if [ "$BRIDGE_READY" != true ]; then
  kill -TERM "$BRIDGE_PID" 2>/dev/null || true
  while bridge_alive; do sleep 0.2; done
  echo "candidate did not take new connections; stable binary unchanged; old generation keeps serving" >&2
  exit 1
fi
if [ "$MODE" = new ]; then
  cp "$STABLE" "$STABLE.previous"
fi
replace_stable "$TARGET_BINARY"

# Old generation: drain admitted work and exit; the supervisor respawns the
# stable path, which now holds the target binary.
kill -TERM "$OLD_PID"
while kill -0 "$OLD_PID" 2>/dev/null; do sleep 0.5; done

# Wait for the supervisor's respawn to serve, then retire the bridge.
SUCCESSOR_READY=false
for _ in $(seq 1 300); do
  STATUS="$(deploy_status || true)"
  PID="$(status_field "$STATUS" pid)"
  if [ -n "$PID" ] && [ "$PID" != "$BRIDGE_PID" ] && [ "$PID" != "$OLD_PID" ] &&
    [ "$(status_field "$STATUS" revision)" = "$TARGET_REVISION" ]; then
    SUCCESSOR_READY=true
    break
  fi
  sleep 0.2
done
[ "$SUCCESSOR_READY" = true ] || { echo "supervisor respawn did not appear; bridge stays in service" >&2; exit 1; }

kill -TERM "$BRIDGE_PID"
while bridge_alive; do sleep 0.2; done
echo "serving generation: pid $PID revision $TARGET_REVISION"
