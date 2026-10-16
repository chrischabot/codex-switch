#!/usr/bin/env bash

release_script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(cd "$release_script_dir/../.." && pwd)"
release_repository="joshp123/codex-switch"
production_feed="https://github.com/$release_repository/releases/latest/download/appcast.xml"
release_config="${CODEX_SWITCH_RELEASE_CONFIG_DIR:-${XDG_CONFIG_HOME:-$HOME/.config}/codex-switch/release}"
notary_config="${OPENTRAWL_RELEASE_CONFIG_DIR:-${XDG_CONFIG_HOME:-$HOME/.config}/opentrawl/release}"
notary_key="$notary_config/app-store-connect-api-key.p8"
notary_key_id_file="$notary_config/app-store-connect-key-id"
notary_issuer_file="$notary_config/app-store-connect-issuer-id"
sparkle_private_key="$release_config/sparkle-ed25519-private-key"
sparkle_public_key_file="$repository_root/deploy/SparklePublicKey"

fail() { echo "$(basename "$0"): $*" >&2; exit 1; }

require_commands() {
  local command
  for command in "$@"; do
    command -v "$command" >/dev/null || fail "required command is missing: $command"
  done
}

require_notary_credentials() {
  [[ -s "$notary_key" && -s "$notary_key_id_file" && -s "$notary_issuer_file" ]] ||
    fail "App Store Connect team key is missing from $notary_config"
  notary_key_id="$(tr -d '[:space:]' < "$notary_key_id_file")"
  notary_issuer="$(tr -d '[:space:]' < "$notary_issuer_file")"
  xcrun notarytool history --key "$notary_key" --key-id "$notary_key_id" --issuer "$notary_issuer" >/dev/null
}

derive_sparkle_public_key() {
  [[ -s "$sparkle_private_key" ]] || fail "restore the Sparkle private key, or run setup-release-credentials --create-sparkle-key before the first release"
  local seed_hex
  seed_hex="$(openssl base64 -d -A -in "$sparkle_private_key" | xxd -p -c 256)"
  [[ ${#seed_hex} -eq 64 ]] || fail "Sparkle private key must be base64 for exactly 32 bytes"
  derived_sparkle_public_key="$(
    printf '302e020100300506032b657004220420%s' "$seed_hex" | xxd -r -p |
      openssl pkey -inform DER -pubout -outform DER | tail -c 32 | openssl base64 -A
  )"
  [[ -n "$derived_sparkle_public_key" ]] || fail "Ed25519 requires OpenSSL 3; run through devenv shell"
}

verify_update_signature() {
  local archive="$1" public_key="$2" signature="$3"
  openssl pkeyutl -verify -rawin -pubin -keyform DER \
    -inkey <(printf '302a300506032b6570032100' | xxd -r -p; printf '%s' "$public_key" | openssl base64 -d -A) \
    -sigfile <(printf '%s' "$signature" | openssl base64 -d -A) \
    -in "$archive" >/dev/null || fail "Sparkle archive signature does not match the app public key"
}

valid_feed_url() {
  [[ "$1" == "$production_feed" || "$1" =~ ^http://127\.0\.0\.1:[0-9]+/[^\?#]+\.xml$ ]]
}
