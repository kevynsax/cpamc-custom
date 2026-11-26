// CUSTOM (cpamc-custom)
import fs from 'node:fs';
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';

try {
  const parameters = new Map();
  const args = process.argv.slice(2);
  for (let i = 0; i < args.length; i += 2) {
    if (!['--gateway', '--management-key-file', '--client', '--out'].includes(args[i]) || !args[i + 1]) throw new Error('Invalid arguments');
    parameters.set(args[i], args[i + 1]);
  }
  for (const required of ['--gateway', '--management-key-file', '--client', '--out']) {
    if (!parameters.has(required)) throw new Error('Missing arguments');
  }
  const gatewayArgument = parameters.get('--gateway');
  const gateway = new URL(gatewayArgument);
  if (gateway.protocol !== 'https:' || gateway.username || gateway.password) throw new Error('HTTPS gateway required');
  const keyFile = parameters.get('--management-key-file');
  const keyData = fs.readFileSync(keyFile, 'utf8');
  const key = keyData.trim();
  const output = parameters.get('--out');
  if (fs.existsSync(output)) throw new Error('Output already exists');
  const securityArgs = ['find-generic-password', '-s', 'gemini', '-a', 'antigravity', '-w'];
  const readOptions = { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] };
  const stored = execFileSync('/usr/bin/security', securityArgs, readOptions);
  const encoded = stored.trim();
  if (!encoded.startsWith('go-keyring-base64:')) throw new Error('Unrecognized agy sign-in');
  const base64 = encoded.slice('go-keyring-base64:'.length);
  const bytes = Buffer.from(base64, 'base64');
  const decoded = bytes.toString('utf8');
  const entry = JSON.parse(decoded);
  const token = entry.token;
  const email = entry.id_token_email;
  if (!email || !token.access_token || !token.refresh_token || token.refresh_token.startsWith('cpamc-agy-') || !token.expiry) throw new Error('A native agy Google sign-in is required');
  const digest = createHash('sha256');
  digest.update(email);
  const emailHash = digest.digest('hex');
  let accountID = `antigravity-agy-${emailHash.slice(0, 20)}.json`;
  const payload = { type: 'antigravity', email, access_token: token.access_token, refresh_token: token.refresh_token, expired: token.expiry, expires_in: token.expires_in, timestamp: Date.now(), agy_proxy_only: true };
  const payloadJSON = JSON.stringify(payload);
  const headers = { Authorization: `Bearer ${key}`, 'Content-Type': 'application/json' };
  const credentialURL = new URL('/v8/management/credentials', gateway);
  credentialURL.searchParams.set('name', accountID);
  const listURL = new URL('/v8/management/credentials', gateway);
  const listOptions = { headers };
  const listed = await fetch(listURL, listOptions);
  if (!listed.ok) throw new Error('Credential inventory rejected');
  const inventory = await listed.json();
  const accountEmail = email.toLowerCase();
  const matchesAccount = file => {
    const candidateEmail = file.email?.toLowerCase();
    return file.provider === 'antigravity' && candidateEmail === accountEmail;
  };
  const matches = inventory.files.filter(matchesAccount);
  if (matches.length > 1) throw new Error('Resolve duplicate accounts in the panel first');
  if (matches.length === 1) {
    const existing = matches[0];
    if (existing.disabled || existing.status === 'disabled') throw new Error('Account is disabled');
    accountID = existing.id;
    const fields = { name: existing.name, agy_proxy_only: true };
    const fieldsJSON = JSON.stringify(fields);
    const fieldsOptions = { method: 'PATCH', headers, body: fieldsJSON };
    const fieldsURL = new URL('/v8/management/credentials/fields', gateway);
    const patched = await fetch(fieldsURL, fieldsOptions);
    if (!patched.ok) throw new Error('Could not mark existing account as agy-only');
  } else {
    const uploadOptions = { method: 'POST', headers, body: payloadJSON };
    const uploaded = await fetch(credentialURL, uploadOptions);
    if (!uploaded.ok) throw new Error('Credential upload rejected');
  }
  const bootstrapRequest = { client: parameters.get('--client'), account_id: accountID };
  const bootstrapJSON = JSON.stringify(bootstrapRequest);
  const bootstrapOptions = { method: 'POST', headers, body: bootstrapJSON };
  const bootstrapURL = new URL('/v8/management/access/agy/bootstrap', gateway);
  const response = await fetch(bootstrapURL, bootstrapOptions);
  if (!response.ok) throw new Error('Bootstrap rejected; the held token may need the normal refresh loop to finish');
  const data = await response.text();
  const writeOptions = { mode: 0o600, flag: 'wx' };
  fs.writeFileSync(output, data, writeOptions);
  console.log('Imported the current agy account as proxy-only and wrote an owner-only bootstrap file. No local sign-in was changed.');
} catch {
  console.error('Enrollment failed. Check arguments, management access, native agy sign-in and held-account refresh status. Credentials were not printed.');
  process.exitCode = 1;
}
