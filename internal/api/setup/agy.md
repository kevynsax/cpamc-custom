# agy through CPAMC

<!-- CUSTOM (cpamc-custom) -->

CPAMC holds the Google account currently signed into agy on this Mac. Only agy uses it:
the account is marked `agy_proxy_only` and is excluded from model routing, model discovery
and credit probes. CPAMC's existing OAuth refresh loop owns Google's refresh token.

The public gateway is `https://ai-models.kevyn.com.br`. Its AGY proxy address is
`https://agy.kevyn.com.br`: TLS passes through the public entry point to CPAMC's
dedicated port 8444, without passing CONNECT through the HTTP gateway.

agy receives a short-lived Google access token and a random **CPAMC proxy refresh credential**.
That credential is not Google's refresh token. CPAMC accepts it only for its bound client
key and account; it never sends it, or agy's compiled-in client secret, to Google.
Disabled/deleted accounts and unknown/revoked credentials receive `invalid_grant` on the
next refresh. An account waiting for its normal OAuth refresh receives
`temporarily_unavailable`; the proxy does not run another refresh implementation.

## Gateway administrator

In the panel's **Add API Key** or **Edit API Key** dialog, select **AGY** under client
application. The speed-tier picker is disabled because it does not apply to the proxy.
Click **Add** or **Update**, then **Save changes**. This saves both the client key and
its Google-account authorization; no source-editor change is needed. Existing clients
keep their client identity when their label or key is edited. Deleting a key or changing
it back to **General** removes its AGY authorization when the configuration is saved.

For an enrolled account, click **AGY setup file** beside the saved key, select the
Google account, then **Download setup file**. Disabled accounts cannot be selected;
accounts waiting for token refresh can be reloaded shortly. If no account appears,
complete the one-time enrollment below on the Mac already signed into agy, then
return here and click **Reload accounts**. The picker lists only AGY-only accounts
and does not change account credentials or run model requests.

The downloaded `agy-bootstrap-<client>.json` is the bootstrap file used by the Mac
installer's `--bootstrap` argument. Transfer it and the corresponding client key
securely to the new Mac. It includes a short-lived access token and CPAMC's synthetic
refresh credential, not Google's real refresh token or the management key. Install
promptly, then delete the file. If it expires, download a fresh one from the panel.
Each download replaces that client's previous AGY login: use a separate key per Mac.

Configure v8 YAML (use a valid TLS certificate for the proxy hostname):

```yaml
server:
  agy-proxy:
    listen: ":8444"
    cert: /CLIProxyAPI/agy-tls/tls.crt
    key: /CLIProxyAPI/agy-tls/tls.key
    state-dir: /CLIProxyAPI/agy-proxy
access:
  api-keys:
    - CLIENT_KEY
  google-account-clients:
    - name: agy
      api-key: CLIENT_KEY
```

The dedicated port speaks **TLS-wrapped HTTP/1.1 CONNECT**, not plaintext HTTP. Expose it
directly or through TCP/TLS passthrough, not an ordinary HTTP ingress. Its certificate
must validate for the proxy hostname. The gateway generates its interception CA in
`state-dir/ca.pem` (private, includes its key). Persist and protect this directory and
`grants.yaml`; never publish or copy their private contents to clients. Use a single
state-directory writer. The public certificate alone is served at `/setup/agy-ca.pem`.
Listener, certificate and state-directory changes require a server restart. Client-key
changes take effect on existing tunnels' next request.

Before the user installer replaces the local Keychain entry, run enrollment **on the Mac
currently signed into agy**, with an owner-only file containing the management key:

```bash
curl -fsSL https://HOST/setup/agy-enroll.mjs -o /tmp/agy-enroll.mjs
node /tmp/agy-enroll.mjs --gateway https://HOST \
  --management-key-file /PRIVATE/PATH/management-key --client agy \
  --out /PRIVATE/PATH/agy-bootstrap.json
```

This reads `gemini` / `antigravity` from macOS Keychain. It reuses a matching account in
CPAMC (preserving its credentials and setting `agy_proxy_only`), or uploads that same
account as an agy-only credential through the authenticated v8 credentials endpoint, then calls
`POST /v8/management/access/agy/bootstrap` with `{ "client": "agy", "account_id": "..." }`.
No model request is made. The local sign-in is unchanged by enrollment. If the access
token is near expiry, wait for CPAMC's existing refresh loop and issue bootstrap again.
The bootstrap response contains `access_token`, `token_type`, `expires_in`, `expiry`,
`id_token_email`, and the synthetic `refresh_token`; it never contains Google's refresh token.
Keep this file private and give it only to the selected client. Issuing bootstrap again
revokes that client's previous proxy refresh credential. Manage account enable/disable
in the existing panel. Deleting the client, removing its key from `access.api-keys`, or
changing its key also revokes access.

Do not upload a second, unrestricted copy of this account. Enrollment refuses disabled
or duplicate matching accounts; resolve those in the panel first. Do not run the
inference verification action for this account.

## User installation (macOS)

Requires Node.js 22+ (`brew install node` if missing). Fully quit Antigravity and agy first.
Save the client key in an owner-only file; use the administrator's fresh bootstrap file:

```bash
curl -fsSL https://HOST/setup/install-agy.sh | bash -s -- \
  --gateway https://HOST --proxy https://agy.kevyn.com.br \
  --key-file /PRIVATE/PATH/client-key --bootstrap /PRIVATE/PATH/agy-bootstrap.json
```

Add `--dry-run` to print the plan without changing anything. `--local-port 17891` changes
the default local connector port if it is occupied.

The installer downloads agy from Google's CLI installer and the Antigravity app from
Google's official download page when missing; existing installations are preserved.
It asks for administrator approval to trust **this proxy's CA** in the macOS System
Keychain. This permits CPAMC to decrypt HTTPS traffic routed through its proxy. Do not
install a CA from an untrusted gateway. The installer replaces the shared
`gemini` / `antigravity` Keychain sign-in with the bootstrap entry. It does not retain a
copy of Google's refresh token on this Mac.

A login LaunchAgent runs a small CONNECT-only connector on `127.0.0.1:17891`; it sends
proxy authentication to CPAMC over TLS. It does not decrypt traffic. Keys and config
are owner-only under `~/.config/cpamc/agy`. The terminal `agy` alias (new terminal) and
`~/.local/bin/agy-cpamc` launch the actual agy binary through this connector.
Open **~/Applications/Antigravity CPAMC.app** for the app. This launcher supplies the
same proxy to Electron and its backend, without modifying Google's signed app.
Quit any existing Antigravity process before opening it. Opening the original app or
the agy binary by absolute path bypasses these launchers. No system-wide proxy is set.

After installing, delete the bootstrap file. Changed configuration/launcher files are
backed up as `.cpamc-bak-<time>`; the replaced Keychain sign-in is intentionally not backed
up because it contains Google's refresh token. To stop using CPAMC, unload its LaunchAgent,
remove the CPAMC agy aliases and launchers, remove this CA from System Keychain, and sign
back into agy normally. Other applications and their sign-ins are not configured by this kit.

## Traffic and logs

Each tunnel serves HTTP/2 or HTTP/1.1 and forwards to the original HTTPS host, path,
headers and body. Only `POST https://oauth2.googleapis.com/token` is answered locally.
Userinfo and all other public HTTPS destinations are forwarded; private-network
destinations and ports other than 443 are refused. Proxy credentials are transport-only
and are never forwarded to destination servers. There is no prompt rewriting, provider
translation, payload configuration or automatic retry on this path.

Full request/response content, status and headers are recorded in the existing
`logs/clients/<client>` store, including locally answered token requests. Supported content
encodings (including gzip) are decoded only for logs. The decoded, masked response body is
in the API RESPONSE section; the RESPONSE section holds its original status and headers.
Authorization, cookies, tokens and client secrets are masked in log copies,
not in traffic. Streaming responses reach agy as they arrive; the complete exchange is
written when it ends. Ambiguous duplicate JSON keys are masked conservatively.
Existing per-client retention limits apply. Read logs through
`GET /v8/management/observability/logs/clients` and its client/file routes.

The kit is installed by the user. Publishing these instructions does not install it,
trust a CA, enroll an account, expose a port or deploy the gateway automatically.
