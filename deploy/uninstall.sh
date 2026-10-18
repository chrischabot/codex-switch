#!/bin/sh
# Removes the proxy, its launchd job and the menu app. Sign-ins and usage
# history stay in the data directory.
set -eu

repository=$(CDPATH='' cd -P "$(dirname "$0")/.." && pwd)
. "$repository/deploy/installation.sh"

# Codex, Claude and Pi would fail every request once the proxy is gone.
if [ -f "$data/client-capability" ] &&
  { /usr/bin/grep -qsF -f "$data/client-capability" "$HOME/.codex/config.toml" "$HOME/.pi/agent/models.json" "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/settings.json" "${CLAUDE_CONFIG_DIR:-$HOME/.claude}/settings.local.json" ||
    printf '%s\n' "${HTTPS_PROXY:-}" "${HTTP_PROXY:-}" "${https_proxy:-}" "${http_proxy:-}" | /usr/bin/grep -qsF -f "$data/client-capability"; }; then
  echo "A client still uses Codex Switch. Remove its proxy settings from Codex, Claude user settings and Desktop local environments, and Pi; restart those clients, then run this again." >&2
  exit 1
fi

# Desktop stores local environment overrides privately. Do not read its secrets
# or assume that removing settings.json proves those overrides are gone.
if [ -f "$data/claude-proxy-ca.pem" ] && [ "${CODEX_SWITCH_CLAUDE_DISCONNECTED:-0}" != 1 ]; then
  echo "Remove the Codex Switch proxy and CA from Claude Desktop local environments, restart affected sessions, then rerun with CODEX_SWITCH_CLAUDE_DISCONNECTED=1. Claude settings files are checked separately." >&2
  exit 1
fi

# The proxy finishes the requests it has already accepted, then exits.
/bin/launchctl bootout "gui/$(id -u)/$label" 2>/dev/null || true
quit_menu_app
for installed in "$plist" "$(dirname "$binary")" "$command_link" "$app"; do
  if [ -e "$installed" ] || [ -L "$installed" ]; then
    /usr/bin/trash "$installed"
  fi
done

echo "Codex Switch is uninstalled. Your sign-ins and usage history are in $data; move that folder to the Trash to remove them."
