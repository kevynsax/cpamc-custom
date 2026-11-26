// CUSTOM (cpamc-custom)
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import http from 'node:http';
import tls from 'node:tls';
import { spawn, execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const home = os.homedir();
const directory = path.join(home, '.config/cpamc/agy');
const configPath = path.join(directory, 'config.json');
const sourcePath = fileURLToPath(import.meta.url);
const [command, ...args] = process.argv.slice(2);

class SetupError extends Error {}

function run(program, argumentsList, extra = {}) {
  const options = { stdio: 'inherit', ...extra };
  const child = spawn(program, argumentsList, options);
  const awaitExit = (resolve, reject) => {
    child.once('error', reject);
    const onExit = code => {
      if (code !== 0) {
        const error = new SetupError('A required installation or launch command failed.');
        reject(error);
        return;
      }
      resolve();
    };
    child.once('exit', onExit);
  };
  return new Promise(awaitExit);
}

function readConfig() {
  const data = fs.readFileSync(configPath, 'utf8');
  const config = JSON.parse(data);
  return config;
}

function privateWrite(target, data, mode = 0o600) {
  if (fs.existsSync(target)) {
    const backup = `${target}.cpamc-bak-${Date.now()}`;
    fs.copyFileSync(target, backup);
    fs.chmodSync(backup, mode);
  }
  const options = { mode };
  fs.writeFileSync(target, data, options);
  fs.chmodSync(target, mode);
}

function quote(value) {
  const escaped = value.replaceAll("'", "'\\''");
  return `'${escaped}'`;
}

function xml(value) {
  const escaped = value.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&quot;');
  return escaped;
}

async function fetchBytes(address) {
  const response = await fetch(address);
  if (!response.ok) throw new SetupError(`Download failed (HTTP ${response.status})`);
  const buffer = await response.arrayBuffer();
  return Buffer.from(buffer);
}

function bridge() {
  const config = readConfig();
  const remote = new URL(config.proxy);
  const keyData = fs.readFileSync(config.keyFile, 'utf8');
  const key = keyData.trim();
  const authBytes = Buffer.from(`agy:${key}`);
  const authorization = authBytes.toString('base64');
  const server = http.createServer();
  const rejectPlainHTTP = (_request, response) => {
    response.writeHead(405);
    response.end('CONNECT required');
  };
  server.on('request', rejectPlainHTTP);
  const connect = (request, client, head) => {
    if (!/^(?:[a-z0-9._-]+|\[[a-f0-9:.]+\]):443$/i.test(request.url)) {
      client.end('HTTP/1.1 403 Forbidden\r\n\r\n');
      return;
    }
    const options = { host: remote.hostname, port: Number(remote.port || 443), servername: remote.hostname, ALPNProtocols: ['http/1.1'], rejectUnauthorized: true };
    const upstream = tls.connect(options);
    let pending = Buffer.alloc(0);
    let connected = false;
    const authenticate = () => {
      upstream.write(`CONNECT ${request.url} HTTP/1.1\r\nHost: ${request.url}\r\nProxy-Authorization: Basic ${authorization}\r\n\r\n`);
    };
    upstream.once('secureConnect', authenticate);
    function handshake(chunk) {
      const chunks = [pending, chunk];
      pending = Buffer.concat(chunks);
      const end = pending.indexOf('\r\n\r\n');
      if (end < 0) {
        if (pending.length > 65536) upstream.destroy();
        return;
      }
      const status = pending.subarray(0, end).toString('ascii');
      if (!/^HTTP\/1\.[01] 200\b/.test(status)) {
        client.end('HTTP/1.1 407 Proxy Authentication Required\r\n\r\n');
        upstream.destroy();
        return;
      }
      connected = true;
      upstream.off('data', handshake);
      client.write('HTTP/1.1 200 Connection Established\r\n\r\n');
      const remainder = pending.subarray(end + 4);
      if (remainder.length) client.write(remainder);
      if (head.length) upstream.write(head);
      client.pipe(upstream);
      upstream.pipe(client);
    }
    upstream.on('data', handshake);
    const upstreamError = () => {
      if (!connected) client.end('HTTP/1.1 502 Bad Gateway\r\n\r\n');
      client.destroy();
    };
    const closeUpstream = () => {
      upstream.destroy();
    };
    const closeClient = () => {
      client.destroy();
    };
    upstream.on('error', upstreamError);
    client.on('error', closeUpstream);
    client.on('close', closeUpstream);
    upstream.on('close', closeClient);
  };
  server.on('connect', connect);
  const listenError = () => {
    console.error('CPAMC agy connector could not listen. Check the LaunchAgent and port.');
    process.exitCode = 1;
  };
  server.on('error', listenError);
  server.listen(config.localPort, '127.0.0.1');
}

async function launch(app) {
  const config = readConfig();
  const local = `http://127.0.0.1:${config.localPort}`;
  const environment = { ...process.env, HTTPS_PROXY: local, HTTP_PROXY: local, https_proxy: local, http_proxy: local, ALL_PROXY: local, all_proxy: local, NO_PROXY: 'localhost,127.0.0.1,::1', no_proxy: 'localhost,127.0.0.1,::1', NODE_EXTRA_CA_CERTS: config.caFile, SSL_CERT_FILE: config.caFile };
  const options = { env: environment };
  if (app) {
    const statusArgs = ['-x', 'Antigravity'];
    const status = spawn('/usr/bin/pgrep', statusArgs);
    const awaitStatus = resolve => {
      const exited = code => { resolve(code === 0); };
      status.once('exit', exited);
    };
    const running = await new Promise(awaitStatus);
    if (running) throw new SetupError('Fully quit Antigravity before opening Antigravity CPAMC.');
    const appArgs = [`--proxy-server=${local}`, '--proxy-bypass-list=localhost;127.0.0.1;[::1]', ...args];
    await run(config.app, appArgs, options);
    return;
  }
  await run(config.cli, args, options);
}

async function install() {
  const parameters = new Map();
  for (let i = 0; i < args.length; i++) {
    const name = args[i];
    if (name === '--dry-run') {
      parameters.set(name, true);
      continue;
    }
    if (!['--gateway', '--proxy', '--key-file', '--bootstrap', '--local-port'].includes(name) || !args[i + 1]) throw new SetupError('Unknown or missing installer argument; see install-agy.sh --help.');
    parameters.set(name, args[++i]);
  }
  const nodeVersionParts = process.versions.node.split('.');
  const nodeMajorVersion = Number(nodeVersionParts[0]);
  if (process.platform !== 'darwin' || nodeMajorVersion < 22) throw new SetupError('macOS and Node.js 22+ are required.');
  for (const required of ['--gateway', '--proxy', '--key-file', '--bootstrap']) {
    if (!parameters.has(required)) throw new SetupError(`Missing ${required}`);
  }
  const gatewayArgument = parameters.get('--gateway');
  const proxyArgument = parameters.get('--proxy');
  const gateway = new URL(gatewayArgument);
  const proxy = new URL(proxyArgument);
  if (gateway.protocol !== 'https:' || proxy.protocol !== 'https:' || gateway.username || gateway.password || proxy.username || proxy.password || proxy.pathname !== '/') throw new SetupError('Use HTTPS gateway and proxy URLs without credentials or paths.');
  const keyFile = parameters.get('--key-file');
  const keyData = fs.readFileSync(keyFile, 'utf8');
  const key = keyData.trim();
  if (!key || /[\r\n]/.test(key)) throw new SetupError('Invalid client key file.');
  const bootstrapPath = parameters.get('--bootstrap');
  const bootstrapData = fs.readFileSync(bootstrapPath, 'utf8');
  const bootstrap = JSON.parse(bootstrapData);
  const expiry = Date.parse(bootstrap.expiry);
  if (!bootstrap.access_token || !bootstrap.refresh_token?.startsWith('cpamc-agy-') || !bootstrap.id_token_email || !(expiry > Date.now() + 120000)) throw new SetupError('Invalid or expired bootstrap file; ask the administrator to issue it again.');
  const portArgument = parameters.get('--local-port') || 17891;
  const localPort = Number(portArgument);
  if (!Number.isInteger(localPort) || localPort < 1024 || localPort > 65535) throw new SetupError('Invalid local port.');
  if (parameters.has('--dry-run')) {
    console.log('- Would install agy and Antigravity, trust the proxy CA, seed the gemini/antigravity Keychain entry, and install scoped terminal/app launchers and a login connector.');
    return;
  }
  const authenticateProxy = (resolve, reject) => {
    const options = { host: proxy.hostname, port: Number(proxy.port || 443), servername: proxy.hostname, ALPNProtocols: ['http/1.1'], rejectUnauthorized: true };
    const socket = tls.connect(options);
    let pending = '';
    let answered = false;
    const fail = () => {
      socket.destroy();
      const error = new SetupError('Cannot authenticate to the TLS proxy. Check its URL, certificate, exposed port and client key.');
      reject(error);
    };
    const authenticate = () => {
      const bytes = Buffer.from(`agy:${key}`);
      const authorization = bytes.toString('base64');
      socket.write(`CONNECT oauth2.googleapis.com:443 HTTP/1.1\r\nHost: oauth2.googleapis.com:443\r\nProxy-Authorization: Basic ${authorization}\r\n\r\n`);
    };
    const readStatus = chunk => {
      const text = chunk.toString('ascii');
      pending += text;
      if (pending.length > 65536) {
        fail();
        return;
      }
      if (!pending.includes('\r\n\r\n')) return;
      if (!/^HTTP\/1\.[01] 200\b/.test(pending)) {
        fail();
        return;
      }
      answered = true;
      socket.destroy();
      resolve();
    };
    const closed = () => {
      if (!answered) fail();
    };
    socket.once('secureConnect', authenticate);
    socket.on('data', readStatus);
    socket.once('error', fail);
    socket.once('close', closed);
  };
  await new Promise(authenticateProxy);
  const privateDirectoryOptions = { recursive: true, mode: 0o700 };
  fs.mkdirSync(directory, privateDirectoryOptions);
  fs.chmodSync(directory, 0o700);
  const caAddress = new URL('/setup/agy-ca.pem', gateway);
  const ca = await fetchBytes(caAddress);
  if (!ca.includes('-----BEGIN CERTIFICATE-----') || ca.includes('PRIVATE KEY')) throw new SetupError('Gateway returned an invalid public CA.');
  const caFile = path.join(directory, 'ca.pem');
  privateWrite(caFile, ca);
  const trustArgs = ['security', 'add-trusted-cert', '-d', '-r', 'trustRoot', '-k', '/Library/Keychains/System.keychain', caFile];
  await run('/usr/bin/sudo', trustArgs);
  const cli = path.join(home, '.local/bin/agy');
  if (!fs.existsSync(cli)) {
    const tempRoot = os.tmpdir();
    const tempPrefix = path.join(tempRoot, 'cpamc-agy-cli-');
    const staging = fs.mkdtempSync(tempPrefix);
    const installer = path.join(staging, 'install.sh');
    try {
      const script = await fetchBytes('https://antigravity.google/cli/install.sh');
      privateWrite(installer, script);
      const installArgs = [installer];
      await run('/bin/bash', installArgs);
    } finally {
      const cleanupOptions = { recursive: true, force: true };
      fs.rmSync(staging, cleanupOptions);
    }
  }
  const appBundle = '/Applications/Antigravity.app';
  if (!fs.existsSync(appBundle)) {
    const pageBytes = await fetchBytes('https://antigravity.google/download');
    const page = pageBytes.toString('utf8');
    const arch = process.arch === 'arm64' ? 'arm' : 'x64';
    const pattern = new RegExp(`https://storage\\.googleapis\\.com/antigravity-public/antigravity-hub/[^"<> ]+/darwin-${arch}/Antigravity\\.dmg`);
    const match = page.match(pattern);
    if (!match) throw new SetupError('Official app download not found; install Antigravity from https://antigravity.google/download and rerun.');
    const tempRoot = os.tmpdir();
    const tempPrefix = path.join(tempRoot, 'cpamc-agy-app-');
    const staging = fs.mkdtempSync(tempPrefix);
    const disk = path.join(staging, 'Antigravity.dmg');
    const mount = path.join(staging, 'mount');
    try {
      const data = await fetchBytes(match[0]);
      privateWrite(disk, data);
      const attachArgs = ['attach', '-nobrowse', '-mountpoint', mount, disk];
      await run('/usr/bin/hdiutil', attachArgs);
      try {
        const source = path.join(mount, 'Antigravity.app');
        const copyArgs = ['-R', source, '/Applications/'];
        await run('/bin/cp', copyArgs);
      } finally {
        const detachArgs = ['detach', mount];
        await run('/usr/bin/hdiutil', detachArgs);
      }
    } finally {
      const cleanupOptions = { recursive: true, force: true };
      fs.rmSync(staging, cleanupOptions);
    }
  }
  const app = path.join(appBundle, 'Contents/MacOS/Antigravity');
  const token = { access_token: bootstrap.access_token, token_type: 'Bearer', refresh_token: bootstrap.refresh_token, expiry: bootstrap.expiry, expires_in: bootstrap.expires_in };
  const entry = { token, auth_method: 'consumer', id_token_email: bootstrap.id_token_email };
  const entryJSON = JSON.stringify(entry);
  const entryBytes = Buffer.from(entryJSON);
  const entryBase64 = entryBytes.toString('base64');
  const keychainValue = `go-keyring-base64:${entryBase64}`;
  const seedArgs = ['add-generic-password', '-U', '-s', 'gemini', '-a', 'antigravity', '-w', keychainValue];
  const seedOptions = { stdio: 'pipe' };
  execFileSync('/usr/bin/security', seedArgs, seedOptions);
  const savedKey = path.join(directory, 'client-key');
  privateWrite(savedKey, key);
  const runtime = path.join(directory, 'agy-client.mjs');
  const runtimeData = fs.readFileSync(sourcePath);
  privateWrite(runtime, runtimeData);
  const config = { proxy: proxy.href, keyFile: savedKey, localPort, caFile, cli, app };
  const configData = JSON.stringify(config, null, 2);
  privateWrite(configPath, configData);
  const bin = path.join(home, '.local/bin');
  const directoryOptions = { recursive: true };
  fs.mkdirSync(bin, directoryOptions);
  const nodeQuoted = quote(process.execPath);
  const runtimeQuoted = quote(runtime);
  const launcher = `#!/bin/sh\nexec ${nodeQuoted} ${runtimeQuoted} cli "$@"\n`;
  const launcherPath = path.join(bin, 'agy-cpamc');
  privateWrite(launcherPath, launcher, 0o700);
  const alias = `alias agy=${quote(launcherPath)}`;
  for (const name of ['.zshrc', '.bashrc']) {
    const rc = path.join(home, name);
    const previous = fs.existsSync(rc) ? fs.readFileSync(rc, 'utf8') : '';
    const cleaned = previous.replace(/\n?# >>> CPAMC agy\n[\s\S]*?# <<< CPAMC agy\n?/g, '\n');
    const updated = `${cleaned}\n# >>> CPAMC agy\n${alias}\n# <<< CPAMC agy\n`;
    privateWrite(rc, updated);
  }
  const applications = path.join(home, 'Applications');
  const bundle = path.join(applications, 'Antigravity CPAMC.app/Contents');
  const macOS = path.join(bundle, 'MacOS');
  fs.mkdirSync(macOS, directoryOptions);
  const appLauncher = `#!/bin/sh\nexec ${nodeQuoted} ${runtimeQuoted} app "$@"\n`;
  const appLauncherPath = path.join(macOS, 'AntigravityCPAMC');
  privateWrite(appLauncherPath, appLauncher, 0o700);
  const info = '<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>AntigravityCPAMC</string><key>CFBundleIdentifier</key><string>br.com.kevyn.cpamc.agy</string><key>CFBundleName</key><string>Antigravity CPAMC</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>';
  const infoPath = path.join(bundle, 'Info.plist');
  privateWrite(infoPath, info);
  const agents = path.join(home, 'Library/LaunchAgents');
  fs.mkdirSync(agents, directoryOptions);
  const plist = path.join(agents, 'br.com.kevyn.cpamc.agy.plist');
  const nodeXML = xml(process.execPath);
  const runtimeXML = xml(runtime);
  const agent = `<?xml version="1.0"?><plist version="1.0"><dict><key>Label</key><string>br.com.kevyn.cpamc.agy</string><key>ProgramArguments</key><array><string>${nodeXML}</string><string>${runtimeXML}</string><string>bridge</string></array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/></dict></plist>`;
  privateWrite(plist, agent);
  const uid = process.getuid();
  const domain = `gui/${uid}`;
  const unloadArgs = ['bootout', domain, plist];
  try { execFileSync('/bin/launchctl', unloadArgs, seedOptions); } catch {}
  const loadArgs = ['bootstrap', domain, plist];
  await run('/bin/launchctl', loadArgs);
  console.log('- Installed agy-cpamc and ~/Applications/Antigravity CPAMC.app. Fully quit Antigravity, then use these launchers.');
  console.log('- The gemini/antigravity Keychain entry now contains the gateway bootstrap token, not Google’s refresh token.');
  console.log('- Open a new terminal for the agy alias. Use Antigravity CPAMC, not the original app. Delete the bootstrap file after successful setup.');
}

try {
  if (command === 'bridge') bridge();
  else if (command === 'cli') await launch(false);
  else if (command === 'app') await launch(true);
  else if (command === 'install') await install();
  else throw new SetupError('Use install, bridge, cli or app.');
} catch (error) {
  const message = error instanceof SetupError ? error.message : 'CPAMC agy setup/launch failed. Check the required files, permissions, proxy and installation prerequisites; credentials were not printed.';
  console.error(message);
  process.exitCode = 1;
}
