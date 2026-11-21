#!/usr/bin/env bash
# CPAMC client setup for macOS and Linux. Served by the gateway at /setup/install.sh.
#
#   curl -fsSL https://ai-models.kevyn.com.br/setup/install.sh | bash -s -- --key <client key>
#
# What it does, without touching the person's own accounts or settings:
#   1. Saves the gateway key to ~/.config/cpamc/cli-client-api-key (mode 600).
#   2. Installs Claude Code if missing and adds a `yolo` command that runs it through the
#      gateway with --dangerously-skip-permissions. `claude` itself is left alone when the person
#      already uses it with their own account; otherwise `claude` also points at the gateway.
#   3. Desktop: if Claude Desktop is signed in to a Claude account, sets up Codex instead
#      (a separate `cpamc` profile); otherwise points Claude Desktop at the gateway (macOS).
#   4. Installs the image-gen, concise-responses and taste-skill skills for Claude and Codex.
# Every file it changes is backed up first as <file>.cpamc-bak-<time>. Run again any time.
set -euo pipefail

GATEWAY="https://ai-models.kevyn.com.br"
KEY=""
DESKTOP="auto"
DRY_RUN=0
USAGE_BAR=0

usage() {
  cat <<'EOF'
Usage: install.sh --key <client key> [--desktop auto|claude|codex|none] [--usage-bar] [--dry-run]
  --key       CPAMC client API key (also read from CPAMC_API_KEY)
  --desktop   auto (default): Codex when Claude Desktop is signed in, else Claude Desktop
  --usage-bar macOS: install CPAMC Bar (usage left per subscription) and start it at login
  --dry-run   print the plan and change nothing
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --key) KEY="${2:-}"; shift 2 ;;
    --desktop) DESKTOP="${2:-}"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    --usage-bar) USAGE_BAR=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done
KEY="${KEY:-${CPAMC_API_KEY:-}}"
[ -n "$KEY" ] || { echo "missing --key" >&2; usage >&2; exit 2; }
case "$DESKTOP" in auto|claude|codex|none) ;; *) echo "invalid --desktop: $DESKTOP" >&2; exit 2 ;; esac

OS="$(uname -s)"
STAMP="$(date +%Y%m%d%H%M%S)"
CPAMC_DIR="$HOME/.config/cpamc"
KEY_FILE="$CPAMC_DIR/cli-client-api-key"

say() { printf '%s\n' "$*"; }
note() {
  if [ "$DRY_RUN" -eq 1 ]; then say "- (dry run) $*"; else say "- $*"; fi
}
run() {
  if [ "$DRY_RUN" -eq 1 ]; then say "  would run: $*"; return 0; fi
  "$@"
}
backup() {
  [ -e "$1" ] || return 0
  if [ "$DRY_RUN" -eq 1 ]; then say "  would back up $1"; return 0; fi
  cp -p "$1" "$1.cpamc-bak-$STAMP"
}
fetch() { curl -fsSL "$GATEWAY/setup/$1"; }

# --- 1. Key ---------------------------------------------------------------------------------
say "Checking the key against $GATEWAY ..."
status="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $KEY" "$GATEWAY/v1/models")"
[ "$status" = "200" ] || { echo "The gateway rejected the key (HTTP $status). Nothing was changed." >&2; exit 1; }
if [ "$DRY_RUN" -eq 0 ]; then
  mkdir -p "$CPAMC_DIR"; chmod 700 "$CPAMC_DIR"
  backup "$KEY_FILE"
  ( umask 077; printf '%s\n' "$KEY" > "$KEY_FILE" )
fi
note "Key saved to $KEY_FILE"

# --- 2. Claude Code -------------------------------------------------------------------------
CLAUDE_BIN="$HOME/.local/bin/claude"
if [ ! -x "$CLAUDE_BIN" ] && ! command -v claude >/dev/null 2>&1; then
  say "Installing Claude Code ..."
  if [ "$DRY_RUN" -eq 1 ]; then say "  would run: curl -fsSL https://claude.ai/install.sh | bash"
  else curl -fsSL https://claude.ai/install.sh | bash; fi
  CLAUDE_OWN=0
  note "Installed Claude Code"
else
  [ -x "$CLAUDE_BIN" ] || CLAUDE_BIN="$(command -v claude)"
  CLAUDE_OWN=0
  if [ -s "$HOME/.claude/.credentials.json" ]; then CLAUDE_OWN=1; fi
  if [ "$OS" = "Darwin" ] && security find-generic-password -s "Claude Code-credentials" >/dev/null 2>&1; then CLAUDE_OWN=1; fi
  if [ -n "${ANTHROPIC_API_KEY:-}" ]; then CLAUDE_OWN=1; fi
fi

# The gateway serves exactly these models; the CLI picker reads this cache.
MODELS_SCRIPT="$HOME/.local/bin/claude-cpamc-models.mjs"
if [ "$DRY_RUN" -eq 0 ]; then
  mkdir -p "$HOME/.local/bin"
  fetch claude-cpamc-models.mjs > "$MODELS_SCRIPT.tmp" && mv "$MODELS_SCRIPT.tmp" "$MODELS_SCRIPT"
  chmod 755 "$MODELS_SCRIPT"
fi

if [ "$CLAUDE_OWN" -eq 1 ]; then
  # A separate config folder keeps their sign-in, history and settings out of the gateway sessions.
  WRAP_NAMES="yolo"
  CPAMC_CONFIG_DIR="$HOME/.claude-cpamc"
  note "Claude Code is signed in to its own account: left \`claude\` as is, added \`yolo\` for the gateway (config in ~/.claude-cpamc)"
else
  WRAP_NAMES="claude yolo"
  CPAMC_CONFIG_DIR="$HOME/.claude"
  note "\`claude\` and \`yolo\` run Claude Code through the gateway"
fi
if [ "$DRY_RUN" -eq 0 ]; then
  mkdir -p "$CPAMC_CONFIG_DIR"
  settings="$CPAMC_CONFIG_DIR/settings.json"
  if [ ! -s "$settings" ]; then
    printf '{\n  "skipDangerousModePermissionPrompt": true\n}\n' > "$settings"
  elif ! grep -q skipDangerousModePermissionPrompt "$settings" && command -v node >/dev/null 2>&1; then
    backup "$settings"
    node -e 'const fs=require("fs");const p=process.argv[1];const s=JSON.parse(fs.readFileSync(p,"utf8"));s.skipDangerousModePermissionPrompt=true;fs.writeFileSync(p,JSON.stringify(s,null,2)+"\n")' "$settings"
  fi
fi

BLOCK="$(cat <<EOF
# >>> CPAMC Claude Code
cpamc_claude() {
  local cpamc_key cpamc_status
  IFS= read -r cpamc_key < "\$HOME/.config/cpamc/cli-client-api-key" || { echo "cpamc: missing key at ~/.config/cpamc/cli-client-api-key" >&2; return 1; }
  if command -v node >/dev/null 2>&1 && [ -x "\$HOME/.local/bin/claude-cpamc-models.mjs" ]; then
    CLAUDE_CONFIG_DIR="$CPAMC_CONFIG_DIR" "\$HOME/.local/bin/claude-cpamc-models.mjs" || return 1
  fi
  if [ "\$(id -u)" -ne 0 ]; then set -- --dangerously-skip-permissions "\$@"; fi
  command env -u ANTHROPIC_API_KEY -u CLAUDE_CODE_OAUTH_TOKEN \\
    ANTHROPIC_MODEL="claude-opus-5-5[1m]" \\
    ANTHROPIC_DEFAULT_HAIKU_MODEL="claude-sonnet-5-5" \\
    ANTHROPIC_BASE_URL="$GATEWAY" \\
    ANTHROPIC_AUTH_TOKEN="\$cpamc_key" \\
    CLAUDE_CONFIG_DIR="$CPAMC_CONFIG_DIR" \\
    CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1 \\
    CLAUDE_CODE_SUBPROCESS_ENV_SCRUB=0 \\
    "$CLAUDE_BIN" "\$@"
  cpamc_status=\$?
  unset cpamc_key
  return "\$cpamc_status"
}
EOF
)"
for name in $WRAP_NAMES; do
  BLOCK="$BLOCK"$'\n'"$name() { cpamc_claude \"\$@\"; }"
done
BLOCK="$BLOCK"$'\n'"# <<< CPAMC Claude Code"

write_block() {
  local rc="$1"
  [ -e "$rc" ] || { [ "$DRY_RUN" -eq 1 ] || touch "$rc"; }
  if [ "$DRY_RUN" -eq 1 ]; then say "  would write the CPAMC block to $rc"; return; fi
  backup "$rc"
  awk '/^# >>> CPAMC Claude Code$/{skip=1} !skip{print} /^# <<< CPAMC Claude Code$/{skip=0}' "$rc" > "$rc.cpamc-tmp"
  printf '\n%s\n' "$BLOCK" >> "$rc.cpamc-tmp"
  mv "$rc.cpamc-tmp" "$rc"
}
for rc in "$HOME/.zshrc" "$HOME/.bashrc"; do
  if [ -e "$rc" ] || { [ "$rc" = "$HOME/.zshrc" ] && [ "$OS" = "Darwin" ]; } || { [ "$rc" = "$HOME/.bashrc" ] && [ "$OS" = "Linux" ]; }; then
    write_block "$rc"
    note "Shell commands added to $rc"
  fi
done

# Claude Code reads the gateway model list only when this cache exists; prime it now.
if command -v node >/dev/null 2>&1; then
  run env CLAUDE_CONFIG_DIR="$CPAMC_CONFIG_DIR" "$MODELS_SCRIPT"
else
  note "Node.js is not installed: install it (https://nodejs.org) so the CLI model picker and image-gen work"
fi

# --- 3. Desktop -----------------------------------------------------------------------------
claude_desktop_signed_in() {
  local cfg="$HOME/Library/Application Support/Claude/config.json"
  [ -f "$cfg" ] && grep -q '"oauth:tokenCache' "$cfg"
}
codex_signed_in() { [ -s "$HOME/.codex/auth.json" ]; }

if [ "$DESKTOP" = "auto" ]; then
  if [ "$OS" != "Darwin" ]; then DESKTOP="codex"
  elif claude_desktop_signed_in; then DESKTOP="codex"
  else DESKTOP="claude"; fi
fi

setup_codex() {
  local home="${CODEX_HOME:-$HOME/.codex}"
  if ! command -v codex >/dev/null 2>&1 && [ ! -x "$HOME/.local/bin/codex" ]; then
    if command -v brew >/dev/null 2>&1; then run brew install --cask codex
    elif command -v npm >/dev/null 2>&1; then run npm install -g @openai/codex
    else note "Codex not installed: install Homebrew or Node.js, then run this again"; return; fi
    note "Installed the Codex CLI"
  fi
  if [ "$DRY_RUN" -eq 0 ]; then
    mkdir -p "$home"
    fetch codex-models.json > "$home/cpamc-models.json"
    backup "$home/cpamc.config.toml"
    fetch codex-profile.toml | sed "s#__CODEX_HOME__#$home#g" > "$home/cpamc.config.toml"
  fi
  note "Codex profile \`cpamc\` written to $home/cpamc.config.toml (their own config.toml and sign-in untouched)"
  note "Run Codex through the gateway with: codex --profile cpamc  (the Codex app: pick the cpamc profile if it offers profiles)"
}

setup_claude_desktop() {
  local app="/Applications/Claude.app"
  if [ ! -d "$app" ]; then
    if command -v brew >/dev/null 2>&1; then run brew install --cask claude
    else note "Claude Desktop is not installed: download it from https://claude.com/download and run this again"; return; fi
    note "Installed Claude Desktop"
  fi
  local dir="$HOME/Library/Application Support/Claude-3p/configLibrary"
  local id="c0ffee00-cbac-4c00-8000-00000000cbac"
  local meta="$dir/_meta.json"
  local helper="$CPAMC_DIR/claude-desktop-key.sh"
  if [ "$DRY_RUN" -eq 1 ]; then say "  would write $dir/$id.json and apply it in $meta"; return; fi
  mkdir -p "$dir"
  printf '#!/bin/sh\nIFS= read -r key < "$HOME/.config/cpamc/cli-client-api-key" && printf %%s "$key"\n' > "$helper"
  chmod 700 "$helper"
  fetch claude-desktop.json | sed "s#__HELPER__#$helper#g" > "$dir/$id.json"
  if [ -f "$meta" ]; then
    backup "$meta"
    if ! grep -q "$id" "$meta"; then
      plutil -insert entries -json "{\"id\":\"$id\",\"name\":\"CPAMC\"}" -append "$meta"
    fi
    plutil -replace appliedId -string "$id" "$meta"
  else
    printf '{"appliedId":"%s","entries":[{"id":"%s","name":"CPAMC"}]}\n' "$id" "$id" > "$meta"
  fi
  note "Claude Desktop points at the gateway (profile CPAMC; quit and reopen Claude, then choose Continue without signing in)"
}

case "$DESKTOP" in
  codex) setup_codex ;;
  claude)
    if [ "$OS" = "Darwin" ]; then setup_claude_desktop; else setup_codex; fi ;;
  none) note "Desktop step skipped" ;;
esac

# --- 4. Skills ------------------------------------------------------------------------------
SKILL_FILES="image-gen/SKILL.md image-gen/references/models.md image-gen/scripts/generate-image.mjs concise-responses/SKILL.md taste-skill/SKILL.md"
# Only folders this setup owns: the gateway Claude config, and Codex when the desktop step set it up.
SKILL_ROOTS="$CPAMC_CONFIG_DIR/skills"
[ "$DESKTOP" = "codex" ] && SKILL_ROOTS="$SKILL_ROOTS ${CODEX_HOME:-$HOME/.codex}/skills"
for root in $SKILL_ROOTS; do
  for file in $SKILL_FILES; do
    target="$root/$file"
    if [ "$DRY_RUN" -eq 1 ]; then continue; fi
    mkdir -p "$(dirname "$target")"
    backup "$target"
    fetch "skills/$file" > "$target.cpamc-tmp" && mv "$target.cpamc-tmp" "$target"
  done
  [ "$DRY_RUN" -eq 1 ] || chmod 755 "$root/image-gen/scripts/generate-image.mjs"
  note "Skills image-gen, concise-responses, taste-skill in $root"
done

# --- 5. Usage bar (macOS, only when asked) -------------------------------------------------
setup_usage_bar() {
  local app="/Applications/CPAMC Bar.app"
  if [ "$DRY_RUN" -eq 1 ]; then say "  would install $app and start it at login"; return; fi
  local tmp; tmp="$(mktemp -d)"
  fetch cpamc-bar.zip > "$tmp/cpamc-bar.zip"
  ditto -x -k "$tmp/cpamc-bar.zip" "$tmp"
  pkill -x CPAMCBar 2>/dev/null || true
  rm -rf "$app"
  ditto "$tmp/CPAMC Bar.app" "$app"
  rm -rf "$tmp"
  # Downloaded by curl, so macOS adds no quarantine; clear it anyway in case a proxy did.
  xattr -dr com.apple.quarantine "$app" 2>/dev/null || true
  "$app/Contents/MacOS/CPAMCBar" --configure-from "$KEY_FILE"
  open "$app"
  note "CPAMC Bar installed in /Applications, shows usage left in the menu bar and starts at login"
  note "CPAMC Bar is not signed by Apple (no Developer ID): if macOS blocks it, allow it in System Settings > Privacy & Security > Open Anyway"
  note "If macOS asks, allow CPAMC Bar in System Settings > General > Login Items"
}
if [ "$USAGE_BAR" -eq 1 ]; then
  if [ "$OS" = "Darwin" ]; then setup_usage_bar; else note "--usage-bar is macOS only; skipped"; fi
fi

# --- Check ----------------------------------------------------------------------------------
if [ "$DRY_RUN" -eq 0 ]; then
  reply="$(curl -s -m 60 -H "x-api-key: $KEY" -H 'anthropic-version: 2023-06-01' -H 'content-type: application/json' \
    -d '{"model":"claude-sonnet-5-5","max_tokens":8,"messages":[{"role":"user","content":"Reply with ok"}]}' \
    "$GATEWAY/v1/messages" | grep -c '"type":"message"' || true)"
  if [ "$reply" = "1" ]; then note "Gateway answered a test message"; else note "WARNING: the test message got no answer"; fi
fi

say ""
say "CPAMC setup finished. Open a new terminal, then run: yolo"
