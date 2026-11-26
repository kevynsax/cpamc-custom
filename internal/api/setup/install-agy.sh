#!/usr/bin/env bash
# CUSTOM (cpamc-custom)
set -euo pipefail
set +x

GATEWAY="https://ai-models.kevyn.com.br"
ARGS=()
while [ "$#" -gt 0 ]; do
  case "$1" in
    -h|--help)
      printf '%s\n' 'Usage: install-agy.sh --gateway https://HOST --proxy https://HOST:PORT --key-file FILE --bootstrap FILE [--local-port PORT] [--dry-run]'
      exit 0 ;;
    --gateway) GATEWAY="${2:?missing gateway}"; shift 2 ;;
    *) ARGS+=("$1"); shift ;;
  esac
done
[ "$(uname -s)" = Darwin ] || { echo "This installer requires macOS." >&2; exit 1; }
command -v node >/dev/null || { echo "Install Node.js 22+ first (brew install node)." >&2; exit 1; }
case "$GATEWAY" in https://*) ;; *) echo "An HTTPS gateway is required." >&2; exit 1 ;; esac
SETUP_DIR="$(mktemp -d /tmp/cpamc-agy-setup.XXXXXX)"
trap 'rm -f "$SETUP_DIR/agy-client.mjs"; rmdir "$SETUP_DIR"' EXIT
curl -fsSL "$GATEWAY/setup/agy-client.mjs" -o "$SETUP_DIR/agy-client.mjs"
node "$SETUP_DIR/agy-client.mjs" install --gateway "$GATEWAY" "${ARGS[@]}"
