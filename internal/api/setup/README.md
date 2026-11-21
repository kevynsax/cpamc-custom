# CPAMC setup — instructions for the agent doing it

You were asked to set up the CPAMC gateway (`https://ai-models.kevyn.com.br`) on this computer, with a
client API key the person gave you. Follow these steps exactly. Never print, log or commit the key.

## 1. Ask first (macOS only)

On a Mac, before running anything, ask the person:

> Do you want to see how much usage is left on each subscription, in the menu bar?

If they say yes, ask:

> I can download CPAMC Bar into Applications and have it start at login. Go ahead?

Only if they agree to both, add `--usage-bar` to the install command below. On Windows and Linux, skip
these questions.

## 2. Run the installer

macOS or Linux:

```bash
curl -fsSL https://ai-models.kevyn.com.br/setup/install.sh | bash -s -- --key '<the key>'
# Mac, when the person agreed to the usage bar:
curl -fsSL https://ai-models.kevyn.com.br/setup/install.sh | bash -s -- --key '<the key>' --usage-bar
```

Windows (PowerShell):

```powershell
& ([scriptblock]::Create((irm https://ai-models.kevyn.com.br/setup/install.ps1))) -Key '<the key>'
```

Add `--dry-run` / `-DryRun` first if you want to see the plan without changes. The installer is safe to run
again; every file it changes is backed up first as `<file>.cpamc-bak-<time>`.

## 3. What it does

1. Checks the key against the gateway (stops and changes nothing if it is rejected), then saves it to
   `~/.config/cpamc/cli-client-api-key` (owner-only).
2. Claude Code (CLI), in yolo mode (`--dangerously-skip-permissions`), through the gateway:
   - Installs Claude Code if it is missing.
   - If the person already uses Claude Code with **their own** account, `claude` stays exactly as it was and
     a new `yolo` command runs the gateway version with its own config folder `~/.claude-cpamc`.
   - Otherwise `claude` and `yolo` both run through the gateway.
   - Models: Claude Opus 5.5 (default), Claude Fable 5.2, GPT 6.1 Sol, Grok 4.7, Gemini 4 Neon,
     Muse Wonder 1.0 and Claude Sonnet 5.5 (for small jobs).
3. Desktop app — the one the person does **not** already use with their own account:
   - Claude Desktop signed in to a Claude account → leave it alone, set up **Codex** with a separate
     `cpamc` profile (`codex --profile cpamc`); their own Codex/ChatGPT sign-in and `config.toml` stay as they were.
   - Otherwise (ChatGPT/Codex user, or no desktop AI app) → **Claude Desktop** on the gateway
     (macOS: a "CPAMC" third-party inference profile; no Claude account needed).
   - Override with `--desktop claude|codex|none`.
4. Skills `image-gen` (MiniMax H3 and other image models), `concise-responses` and `taste-skill`, in the
   gateway Claude config's `skills/` folder and in the Codex skills folder when Codex was set up.
5. With `--usage-bar` (macOS): installs CPAMC Bar in `/Applications`, connects it read-only with the same
   key (usage left per subscription, no management access), opens it and registers it to start at login.
   Remove it later with `"/Applications/CPAMC Bar.app/Contents/MacOS/CPAMCBar" --remove-login-item` and
   deleting the app.

## 4. After it runs

- Tell the person to open a **new** terminal and run `yolo`.
- Claude Desktop: fully quit and reopen it; on the welcome screen choose **Continue** (no sign-in). The
  model picker shows the seven models above.
- Report the installer's `- ...` summary lines to the person, plus any `WARNING` line.
- Usage bar: point out the gauge icon in the menu bar; if macOS shows a Login Items notice, tell them to allow it.

## Rules for you

- Do not change any other Claude, Codex or ChatGPT setting, account or sign-in.
- Do not edit the installer's output files by hand; run the installer again instead.
- If the key is rejected, stop and ask the person for a valid one.
- Node.js is needed for the model picker and `image-gen`; if the installer says it is missing, offer to
  install it (Homebrew `brew install node`, or https://nodejs.org) and then run the installer again.

## Files

All served from `https://ai-models.kevyn.com.br/setup/`: `install.sh`, `install.ps1`,
`claude-cpamc-models.mjs`, `codex-profile.toml`, `codex-models.json`, `claude-desktop.json`, `cpamc-bar.zip`, and
`skills/<skill>/...`.
