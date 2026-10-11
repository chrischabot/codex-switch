# Where an installation of Codex Switch lives. Sourced by install.sh and
# uninstall.sh.
#
# Everything is under $HOME except the launchd label, the port and the menu
# app. To run a second, isolated installation beside your own, set HOME to
# another directory and set CODEX_SWITCH_LAUNCHD_LABEL, CODEX_SWITCH_LISTEN and
# CODEX_SWITCH_MENU_APP.
label=${CODEX_SWITCH_LAUNCHD_LABEL:-com.josh.codex-switch.proxy}
new_install_listen=${CODEX_SWITCH_LISTEN:-127.0.0.1:2455}
app=${CODEX_SWITCH_MENU_APP:-/Applications/CodexSwitchMenu.app}
binary="$HOME/.local/lib/codex-switch/codex-switch"
command_link="$HOME/.local/bin/codex-switch"
data="$HOME/Library/Application Support/codex-switch"
plist="$HOME/Library/LaunchAgents/$label.plist"

# Stops the menu app installed at $app, if it is running.
quit_menu_app() {
  menu_executable=$(/bin/realpath "$app/Contents/MacOS/CodexSwitchMenu" 2>/dev/null) || return 0
  menu_pids=$(/bin/ps -axo pid=,comm= | /usr/bin/awk -v executable="$menu_executable" '$2 == executable {print $1}')
  [ -n "$menu_pids" ] || return 0
  kill $menu_pids
  while /bin/ps -p "$(echo $menu_pids | tr ' ' ',')" >/dev/null; do sleep 0.2; done
}
