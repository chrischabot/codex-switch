#!/bin/sh
# First installation and app updates share the same proxy handover.
set -eu
umask 077

deployment_directory=$(CDPATH='' cd -P "$(dirname "$0")" && pwd)
. "$deployment_directory/installation.sh"
candidate=${1:?usage: install-proxy.sh <proxy-binary>}
listen=$new_install_listen
if [ -f "$plist" ]; then
  binary=$(/usr/bin/plutil -extract ProgramArguments.0 raw "$plist")
  listen=$(/usr/bin/plutil -extract ProgramArguments.3 raw "$plist")
  data=$(/usr/bin/plutil -extract EnvironmentVariables.CODEX_SWITCH_DATA_DIR raw "$plist")
fi

/bin/mkdir -p -m 700 "$data"
# Sparkle can replace the app while an old proxy is still draining.
workspace=$(/usr/bin/mktemp -d "$data/proxy-update.XXXXXX")
lock="$data/proxy-install.lock"
owns_lock=false
cleanup() {
  if [ "$owns_lock" = true ]; then
    /bin/rmdir "$lock/$$" "$lock"
  fi
  /usr/bin/trash "$workspace"
}
trap cleanup EXIT
/bin/cp "$candidate" "$workspace/codex-switch"
/bin/cp "$deployment_directory/deploy.sh" "$workspace/deploy.sh"
candidate="$workspace/codex-switch"

# Concurrent app launches wait, then check the generation again.
while ! /bin/mkdir "$lock" 2>/dev/null; do
  owner=""
  for entry in "$lock"/*; do
    [ -d "$entry" ] && owner=${entry##*/}
  done
  if [ -n "$owner" ] && ! kill -0 "$owner" 2>/dev/null; then
    /bin/rmdir "$lock/$owner" "$lock" 2>/dev/null || true
  elif [ -z "$owner" ]; then
    sleep 1
    /bin/rmdir "$lock" 2>/dev/null || true
  else
    sleep 1
  fi
done
/bin/mkdir "$lock/$$"
owns_lock=true

# A concurrent first installation may have created the job while we waited.
if [ -f "$plist" ]; then
  binary=$(/usr/bin/plutil -extract ProgramArguments.0 raw "$plist")
  listen=$(/usr/bin/plutil -extract ProgramArguments.3 raw "$plist")
fi

status_field() { printf '%s' "$1" | /usr/bin/plutil -extract "$2" raw -o - -; }
serving_status() {
  [ -f "$data/client-capability" ] || return 1
  /usr/bin/curl -sf --max-time 2 "http://$listen/client/$(cat "$data/client-capability")/codex-switch/deploy-status"
}
wait_for_proxy() {
  waited=0
  until status=$(serving_status); do
    waited=$((waited + 1))
    [ "$waited" -le 120 ] || { echo "the proxy is not serving on $listen; see $data/proxy.log" >&2; return 1; }
    sleep 1
  done
}
revision=$("$candidate" print-revision)
modified=$("$candidate" print-revision --dirty)
case "$revision" in
  ''|unknown) echo "bundled proxy has no source revision" >&2; exit 1 ;;
esac
if [ -f "$plist" ]; then
  if ! status=$(serving_status); then
    if ! /bin/launchctl print "gui/$(id -u)/$label" >/dev/null 2>&1; then
      /bin/launchctl bootstrap "gui/$(id -u)" "$plist"
    fi
    wait_for_proxy
  fi
  if [ "$(status_field "$status" revision)" = "$revision" ] &&
    [ "$(status_field "$status" dirty)" = "$modified" ] &&
    { [ "$modified" = false ] || /usr/bin/cmp -s "$candidate" "$binary"; }; then
    echo "the bundled proxy is already serving"
  else
    /bin/sh "$workspace/deploy.sh" new "$candidate" --stable "$binary" --data "$data" --listen "$listen"
  fi
else
  /bin/mkdir -p "$(dirname "$binary")" "$(dirname "$plist")"
  /bin/cp "$candidate" "$binary.next"
  /bin/mv -f "$binary.next" "$binary"
  /usr/bin/plutil -create xml1 "$plist"
  /usr/bin/plutil -insert Label -string "$label" "$plist"
  /usr/bin/plutil -insert ProgramArguments -array "$plist"
  for argument in "$binary" serve --listen "$listen"; do
    /usr/bin/plutil -insert ProgramArguments -string "$argument" -append "$plist"
  done
  /usr/bin/plutil -insert EnvironmentVariables -dictionary "$plist"
  /usr/bin/plutil -insert EnvironmentVariables.CODEX_SWITCH_DATA_DIR -string "$data" "$plist"
  /usr/bin/plutil -insert EnvironmentVariables.PATH -string "$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:$PATH" "$plist"
  /usr/bin/plutil -insert RunAtLoad -bool true "$plist"
  /usr/bin/plutil -insert KeepAlive -bool true "$plist"
  /usr/bin/plutil -insert ExitTimeOut -integer 0 "$plist"
  /usr/bin/plutil -insert StandardOutPath -string "$data/launchd.log" "$plist"
  /usr/bin/plutil -insert StandardErrorPath -string "$data/launchd.log" "$plist"
  /bin/launchctl bootstrap "gui/$(id -u)" "$plist"
  wait_for_proxy
fi
/bin/mkdir -p "$(dirname "$command_link")"
/bin/ln -sf "$binary" "$command_link"
echo "Codex Switch is running on $listen"
