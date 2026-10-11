#!/bin/sh
# Installs Codex Switch from this checkout, or updates the installation.
#
# The first run builds the proxy, runs it under a launchd job that keeps it
# running, and installs the menu app. Later runs hand the proxy over with
# deploy.sh, so new work goes to the new build while running work finishes on
# the old one, then rebuild and reopen the menu app.
set -eu

repository=$(CDPATH='' cd -P "$(dirname "$0")/.." && pwd)
. "$repository/deploy/installation.sh"

write_launchd_job() {
  /usr/bin/plutil -create xml1 "$plist"
  /usr/bin/plutil -insert Label -string "$label" "$plist"
  /usr/bin/plutil -insert ProgramArguments -array "$plist"
  for argument in "$binary" serve --listen "$listen"; do
    /usr/bin/plutil -insert ProgramArguments -string "$argument" -append "$plist"
  done
  /usr/bin/plutil -insert EnvironmentVariables -dictionary "$plist"
  /usr/bin/plutil -insert EnvironmentVariables.CODEX_SWITCH_DATA_DIR -string "$data" "$plist"
  # The proxy runs the Codex CLI, which may be installed only on this PATH.
  /usr/bin/plutil -insert EnvironmentVariables.PATH -string "$PATH" "$plist"
  /usr/bin/plutil -insert RunAtLoad -bool true "$plist"
  /usr/bin/plutil -insert KeepAlive -bool true "$plist"
  # launchd must never kill a stopping proxy before it finishes admitted work.
  /usr/bin/plutil -insert ExitTimeOut -integer 0 "$plist"
  /usr/bin/plutil -insert StandardOutPath -string "$data/launchd.log" "$plist"
  /usr/bin/plutil -insert StandardErrorPath -string "$data/launchd.log" "$plist"
}

build=$(/usr/bin/mktemp -d "${TMPDIR:-/tmp}/codex-switch-build.XXXXXX")
trap '/usr/bin/trash "$build"' EXIT
"$repository/scripts/dev/build-proxy" "$build/codex-switch"

/bin/mkdir -p "$(dirname "$binary")" "$(dirname "$command_link")" "$(dirname "$plist")"
/bin/mkdir -p -m 700 "$data"
if [ -f "$plist" ]; then
  listen=$(/usr/bin/plutil -extract ProgramArguments.3 raw "$plist")
  "$repository/deploy/deploy.sh" new "$build/codex-switch" --stable "$binary" --data "$data" --listen "$listen"
else
  listen=$new_install_listen
  /bin/cp "$build/codex-switch" "$binary"
  write_launchd_job
  /bin/launchctl bootstrap "gui/$(id -u)" "$plist"
fi
/bin/ln -sf "$binary" "$command_link"

config_line=$("$binary" print-codex-config --listen "$listen")
status_url="http://$listen/client/$(cat "$data/client-capability")/codex-switch/deploy-status"
waited=0
until /usr/bin/curl -sf --max-time 2 "$status_url" >/dev/null; do
  waited=$((waited + 1))
  [ "$waited" -le 120 ] || { echo "the proxy is not serving on $listen; see $data/proxy.log" >&2; exit 1; }
  sleep 1
done

quit_menu_app
"$repository/macos/CodexSwitchMenu/package_app.sh" --live --destination "$app"
/usr/bin/open -n "$app" --args --data-dir "$data"

echo
echo "Codex Switch is running on $listen. Point Codex at it with this line at the top of ~/.codex/config.toml:"
echo
echo "$config_line"
