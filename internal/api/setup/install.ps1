# CPAMC client setup for Windows. Served by the gateway at /setup/install.ps1.
#
#   & ([scriptblock]::Create((irm https://ai-models.kevyn.com.br/setup/install.ps1))) -Key '<client key>'
#
# Same steps as install.sh: key, Claude Code (yolo through the gateway, own account untouched),
# the desktop app the person does not already use, and the skills. Changed files are backed up
# as <file>.cpamc-bak-<time>.
param(
  [string]$Key = $env:CPAMC_API_KEY,
  [ValidateSet('auto', 'claude', 'codex', 'none')][string]$Desktop = 'auto',
  [switch]$DryRun
)
$ErrorActionPreference = 'Stop'

$Gateway = 'https://ai-models.kevyn.com.br'
$Stamp = Get-Date -Format 'yyyyMMddHHmmss'
$CpamcDir = Join-Path $HOME '.config\cpamc'
$KeyFile = Join-Path $CpamcDir 'cli-client-api-key'

function Note([string]$Text) { Write-Host "- $Text" }
function Backup([string]$Path) {
  if ((Test-Path $Path) -and -not $DryRun) { Copy-Item $Path "$Path.cpamc-bak-$Stamp" -Force }
}
function Fetch([string]$Name) {
  $response = Invoke-WebRequest -UseBasicParsing "$Gateway/setup/$Name"
  return $response.Content
}
function Save([string]$Path, [string]$Content) {
  if ($DryRun) { Write-Host "  would write $Path"; return }
  New-Item -ItemType Directory -Force -Path (Split-Path $Path) | Out-Null
  Backup $Path
  [System.IO.File]::WriteAllText($Path, $Content, (New-Object System.Text.UTF8Encoding($false)))
}

if (-not $Key) { throw 'Missing -Key' }

# 1. Key
try {
  Invoke-WebRequest -UseBasicParsing -Headers @{ Authorization = "Bearer $Key" } "$Gateway/v1/models" | Out-Null
} catch {
  throw 'The gateway rejected the key. Nothing was changed.'
}
Save $KeyFile $Key
if (-not $DryRun) {
  icacls $KeyFile /inheritance:r /grant:r "$($env:USERNAME):(R,W)" | Out-Null
}
Note "Key saved to $KeyFile"

# 2. Claude Code
$ClaudeBin = Join-Path $HOME '.local\bin\claude.exe'
$existing = Get-Command claude -ErrorAction SilentlyContinue
if (-not (Test-Path $ClaudeBin) -and -not $existing) {
  if ($DryRun) { Write-Host '  would install Claude Code' }
  else { Invoke-Expression (Invoke-RestMethod https://claude.ai/install.ps1) }
  Note 'Installed Claude Code'
  $ClaudeOwn = $false
} else {
  if (-not (Test-Path $ClaudeBin)) { $ClaudeBin = $existing.Source }
  $ClaudeOwn = (Test-Path (Join-Path $HOME '.claude\.credentials.json')) -or [bool]$env:ANTHROPIC_API_KEY
}
if ($ClaudeOwn) {
  $ConfigDir = Join-Path $HOME '.claude-cpamc'
  $WrapNames = @('yolo')
  Note 'Claude Code is signed in to its own account: left `claude` as is, added `yolo` for the gateway (config in ~/.claude-cpamc)'
} else {
  $ConfigDir = Join-Path $HOME '.claude'
  $WrapNames = @('claude', 'yolo')
  Note '`claude` and `yolo` run Claude Code through the gateway'
}
$Settings = Join-Path $ConfigDir 'settings.json'
if (-not (Test-Path $Settings)) { Save $Settings "{`n  `"skipDangerousModePermissionPrompt`": true`n}`n" }

$ModelsScript = Join-Path $HOME '.local\bin\claude-cpamc-models.mjs'
Save $ModelsScript (Fetch 'claude-cpamc-models.mjs')

$Block = @"
# >>> CPAMC Claude Code
function cpamc_claude {
  `$cpamcKey = (Get-Content -Raw "`$HOME\.config\cpamc\cli-client-api-key").Trim()
  if (Get-Command node -ErrorAction SilentlyContinue) {
    `$env:CLAUDE_CONFIG_DIR = '$ConfigDir'; node "`$HOME\.local\bin\claude-cpamc-models.mjs"
  }
  `$saved = @{}
  foreach (`$name in 'ANTHROPIC_API_KEY','CLAUDE_CODE_OAUTH_TOKEN','ANTHROPIC_MODEL','ANTHROPIC_DEFAULT_HAIKU_MODEL','ANTHROPIC_BASE_URL','ANTHROPIC_AUTH_TOKEN','CLAUDE_CONFIG_DIR','CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY','CLAUDE_CODE_SUBPROCESS_ENV_SCRUB') {
    `$saved[`$name] = [Environment]::GetEnvironmentVariable(`$name, 'Process')
  }
  try {
    `$env:ANTHROPIC_API_KEY = `$null; `$env:CLAUDE_CODE_OAUTH_TOKEN = `$null
    `$env:ANTHROPIC_MODEL = 'claude-opus-5-5[1m]'
    `$env:ANTHROPIC_DEFAULT_HAIKU_MODEL = 'claude-sonnet-5-5'
    `$env:ANTHROPIC_BASE_URL = '$Gateway'
    `$env:ANTHROPIC_AUTH_TOKEN = `$cpamcKey
    `$env:CLAUDE_CONFIG_DIR = '$ConfigDir'
    `$env:CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY = '1'
    `$env:CLAUDE_CODE_SUBPROCESS_ENV_SCRUB = '0'
    & '$ClaudeBin' --dangerously-skip-permissions @args
  } finally {
    foreach (`$name in `$saved.Keys) { [Environment]::SetEnvironmentVariable(`$name, `$saved[`$name], 'Process') }
  }
}
"@
foreach ($name in $WrapNames) { $Block += "`nfunction $name { cpamc_claude @args }" }
$Block += "`n# <<< CPAMC Claude Code"

$ShellProfile = $PROFILE.CurrentUserAllHosts
$current = if (Test-Path $ShellProfile) { Get-Content -Raw $ShellProfile } else { '' }
$current = [regex]::Replace($current, '(?s)\r?\n?# >>> CPAMC Claude Code.*?# <<< CPAMC Claude Code\r?\n?', "`n")
Save $ShellProfile ($current.TrimEnd() + "`n`n" + $Block + "`n")
Note "PowerShell commands added to $ShellProfile"

if (Get-Command node -ErrorAction SilentlyContinue) {
  if (-not $DryRun) { $env:CLAUDE_CONFIG_DIR = $ConfigDir; node $ModelsScript; Remove-Item Env:CLAUDE_CONFIG_DIR }
} else {
  Note 'Node.js is not installed: install it (winget install OpenJS.NodeJS.LTS) so the CLI model picker and image-gen work'
}

# 3. Desktop
$ClaudeDesktopConfig = Join-Path $env:APPDATA 'Claude\config.json'
$ClaudeDesktopSignedIn = (Test-Path $ClaudeDesktopConfig) -and ((Get-Content -Raw $ClaudeDesktopConfig) -match '"oauth:tokenCache')
if ($Desktop -eq 'auto') { $Desktop = if ($ClaudeDesktopSignedIn) { 'codex' } else { 'claude' } }

$CodexHome = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $HOME '.codex' }
if ($Desktop -eq 'codex') {
  if (-not (Get-Command codex -ErrorAction SilentlyContinue)) {
    if (Get-Command npm -ErrorAction SilentlyContinue) {
      if (-not $DryRun) { npm install -g @openai/codex }
      Note 'Installed the Codex CLI'
    } else { Note 'Codex not installed: install Node.js, then run this again' }
  }
  $catalog = (Join-Path $CodexHome 'cpamc-models.json')
  Save $catalog (Fetch 'codex-models.json')
  $profileText = (Fetch 'codex-profile.toml').Replace('__CODEX_HOME__', $CodexHome.Replace('\', '/'))
  $keyPathToml = $KeyFile.Replace('\', '/')
  $windowsArgs = "args = [`"-NoProfile`", `"-Command`", `"(Get-Content -Raw '$keyPathToml').Trim()`"]"
  $lines = $profileText -split "`n" | ForEach-Object {
    if ($_ -like 'command = *') { 'command = "powershell"' }
    elseif ($_ -like 'args = *') { $windowsArgs }
    else { $_ }
  }
  $profileText = $lines -join "`n"
  Save (Join-Path $CodexHome 'cpamc.config.toml') $profileText
  Note "Codex profile ``cpamc`` written (run: codex --profile cpamc); their own Codex config and sign-in untouched"
} elseif ($Desktop -eq 'claude') {
  $Library = Join-Path $env:LOCALAPPDATA 'Claude-3p\configLibrary'
  $Id = 'c0ffee00-cbac-4c00-8000-00000000cbac'
  $Helper = Join-Path $CpamcDir 'claude-desktop-key.cmd'
  Save $Helper "@powershell -NoProfile -Command `"(Get-Content -Raw '$KeyFile').Trim()`"`r`n"
  $json = (Fetch 'claude-desktop.json').Replace('__HELPER__', $Helper.Replace('\', '\\'))
  Save (Join-Path $Library "$Id.json") $json
  $MetaPath = Join-Path $Library '_meta.json'
  if (Test-Path $MetaPath) {
    $meta = Get-Content -Raw $MetaPath | ConvertFrom-Json
    if (-not ($meta.entries | Where-Object { $_.id -eq $Id })) {
      $meta.entries = @($meta.entries) + @([pscustomobject]@{ id = $Id; name = 'CPAMC' })
    }
    $meta.appliedId = $Id
  } else {
    $meta = [pscustomobject]@{ appliedId = $Id; entries = @([pscustomobject]@{ id = $Id; name = 'CPAMC' }) }
  }
  Save $MetaPath ($meta | ConvertTo-Json -Depth 5)
  if (-not (Get-Command claude -ErrorAction SilentlyContinue) -and -not (Test-Path (Join-Path $env:LOCALAPPDATA 'AnthropicClaude'))) {
    Note 'Claude Desktop is not installed: install it from https://claude.com/download (winget install Anthropic.Claude)'
  }
  Note 'Claude Desktop points at the gateway (profile CPAMC; quit and reopen Claude, then choose Continue)'
} else {
  Note 'Desktop step skipped'
}

# 4. Skills
$SkillFiles = 'image-gen/SKILL.md', 'image-gen/references/models.md', 'image-gen/scripts/generate-image.mjs', 'concise-responses/SKILL.md', 'taste-skill/SKILL.md'
$Roots = @(Join-Path $ConfigDir 'skills')
if ($Desktop -eq 'codex') { $Roots += Join-Path $CodexHome 'skills' }
foreach ($root in $Roots) {
  foreach ($file in $SkillFiles) { Save (Join-Path $root $file) (Fetch "skills/$file") }
  Note "Skills image-gen, concise-responses, taste-skill in $root"
}

Write-Host ''
Write-Host 'CPAMC setup finished. Open a new PowerShell window, then run: yolo'
