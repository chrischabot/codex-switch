#!/bin/zsh

set -euo pipefail

package_dir=${0:A:h}
repo_dir=${package_dir:h:h}
destination="$repo_dir/dist/staged/CodexSwitchStage.app"
result=staged
display_name="Codex Switch Stage"
bundle_identifier=com.josh.codex-switch-menu.stage
running_bundle_name=CodexSwitchStage.app
if (( $# > 0 )); then
    if (( $# != 3 )) || [[ $2 != --destination ]] ||
        [[ $1 != --stage && $1 != --live ]]; then
        print -u2 "usage: $0 [--stage --destination ABSOLUTE_APP_PATH | --live --destination ABSOLUTE_APP_PATH]"
        exit 2
    fi
    [[ $3 == /* ]] || {
        print -u2 "destination must be an absolute path: $3"
        exit 2
    }
    [[ $3 != / && $3 == *.app ]] || {
        print -u2 "destination must name one app bundle: $3"
        exit 2
    }
    destination=$3
    if [[ $1 == --stage ]]; then
        [[ $destination == "$repo_dir/dist/"* ]] || {
            print -u2 "staging destination must be under $repo_dir/dist: $destination"
            exit 2
        }
    else
        result=installed
        display_name="Codex Switch"
        bundle_identifier=com.josh.codex-switch-menu
        running_bundle_name=CodexSwitchMenu.app
    fi
fi

if [[ $result == staged && $destination != "$repo_dir/dist/staged/CodexSwitchStage.app" ]]; then
    stage_name=${${destination:t}%.app}
    display_name="Codex Switch $stage_name"
    bundle_identifier="com.josh.codex-switch-menu.stage.${(L)stage_name}"
fi
running_matching_app=$(
    /bin/ps -axo pid=,comm= | /usr/bin/awk -v executable="$destination/Contents/MacOS/CodexSwitchMenu" '$2 == executable {print $1 " " $2}'
)
[[ -z $running_matching_app ]] || {
    print -u2 "refusing to replace $running_bundle_name while process $running_matching_app is running"
    exit 1
}

staging_root=$(/usr/bin/mktemp -d "${TMPDIR:-/tmp}/codex-switch-menu.XXXXXX")
staged_app="$staging_root/CodexSwitchMenu.app"

cleanup() {
    if [[ -d $staging_root ]]; then
        /usr/bin/trash "$staging_root"
    fi
}
trap cleanup EXIT

/usr/bin/swift build --build-system native --package-path "$package_dir" -c release
bin_dir=$(/usr/bin/swift build --build-system native --package-path "$package_dir" -c release --show-bin-path)
executable="$bin_dir/CodexSwitchMenu"
resource_bundle="$bin_dir/CodexSwitchMenu_CodexSwitchMenu.bundle"

[[ -x $executable ]] || { print -u2 "missing executable: $executable"; exit 1; }
[[ -d $resource_bundle ]] || { print -u2 "missing resource bundle: $resource_bundle"; exit 1; }

/bin/mkdir -p "$staged_app/Contents/MacOS" "$staged_app/Contents/Resources" "$staged_app/Contents/Helpers"
/bin/cp "$executable" "$staged_app/Contents/MacOS/CodexSwitchMenu"
/bin/cp -R "$resource_bundle" "$staged_app/Contents/Resources/"
command -v go >/dev/null || {
    print -u2 "Go is required; run this through the pinned devenv environment"
    exit 1
}
(cd "$repo_dir" && go build -o "$staged_app/Contents/Helpers/codex-switch" ./cmd/codex-switch)
helper="$staged_app/Contents/Helpers/codex-switch"
[[ -x $helper ]] || { print -u2 "missing executable: $helper"; exit 1; }

plist="$staged_app/Contents/Info.plist"
/usr/bin/plutil -create xml1 "$plist"
/usr/bin/plutil -insert CFBundleDisplayName -string "$display_name" "$plist"
/usr/bin/plutil -insert CFBundleExecutable -string CodexSwitchMenu "$plist"
/usr/bin/plutil -insert CFBundleIdentifier -string "$bundle_identifier" "$plist"
/usr/bin/plutil -insert CFBundleName -string CodexSwitchMenu "$plist"
/usr/bin/plutil -insert CFBundlePackageType -string APPL "$plist"
/usr/bin/plutil -insert CFBundleShortVersionString -string 0.1.0 "$plist"
/usr/bin/plutil -insert CFBundleVersion -string 1 "$plist"
/usr/bin/plutil -insert LSMinimumSystemVersion -string 14.0 "$plist"
/usr/bin/plutil -insert LSUIElement -bool true "$plist"
/usr/bin/plutil -insert NSPrincipalClass -string NSApplication "$plist"

/usr/bin/codesign --force --sign - "$helper"
/usr/bin/codesign --verify --strict "$helper"
/usr/bin/codesign --force --sign - "$staged_app"
/usr/bin/codesign --verify --deep --strict "$staged_app"

/bin/mkdir -p "${destination:h}"
if [[ -e $destination ]]; then
    /usr/bin/trash "$destination"
fi
/bin/mv "$staged_app" "$destination"

print "$result $destination"
