// Runs the shipped native archive, never a source build. Node 22.12.0; no deps.
const assert = require('node:assert/strict');
const {createHash} = require('node:crypto');
const {execFileSync, spawn} = require('node:child_process');
const fs = require('node:fs');
const net = require('node:net');
const os = require('node:os');
const path = require('node:path');
const {setTimeout: delay} = require('node:timers/promises');

const platforms = {linux: 'linux', darwin: 'darwin', win32: 'windows'};
const architectures = {x64: 'amd64', arm64: 'arm64'};
const expectedArchives = ['linux', 'darwin', 'windows'].flatMap(platform =>
  ['amd64', 'arm64'].map(arch => `bonsai_${platform}_${arch}.${platform === 'windows' ? 'zip' : 'tar.gz'}`));

function verifyChecksums(dir) {
  const entries = new Map();
  for (const line of fs.readFileSync(path.join(dir, 'checksums.txt'), 'utf8').trim().split(/\r?\n/)) {
    const match = /^([a-f0-9]{64})\s+\*?([^/\\]+)$/.exec(line);
    assert(match, `Invalid checksum line: ${line}`);
    assert(!entries.has(match[2]), `Duplicate checksum: ${match[2]}`);
    entries.set(match[2], match[1]);
    assert.equal(createHash('sha256').update(fs.readFileSync(path.join(dir, match[2]))).digest('hex'), match[1], `Checksum mismatch: ${match[2]}`);
  }
  for (const name of expectedArchives) assert(entries.has(name), `Missing platform archive: ${name}`);
}

function run(binary, args, options) {
  return new Promise((resolve, reject) => {
    const child = spawn(binary, args, {...options, stdio: ['ignore', 'pipe', 'pipe']});
    let stdout = '', stderr = '';
    child.stdout.on('data', data => { stdout += data; });
    child.stderr.on('data', data => { stderr += data; });
    const timer = setTimeout(() => child.kill(), 60_000);
    child.on('error', reject);
    child.on('close', code => {
      clearTimeout(timer);
      if (code !== 0) reject(new Error(`${args.join(' ')} exited ${code}\n${stdout}\n${stderr}`));
      else resolve(stdout);
    });
  });
}

async function waitFor(check, message) {
  for (let attempt = 0; attempt < 100; attempt++) {
    if (await check()) return;
    await delay(100);
  }
  throw new Error(message);
}

async function freePort() {
  const server = net.createServer();
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const port = server.address().port;
  await new Promise(resolve => server.close(resolve));
  return port;
}

function alive(pid) {
  if (!pid) return false;
  // kill(0) sees Unix zombies; ps treats those exited processes as stopped.
  if (process.platform !== 'win32') {
    try { return !/^Z/.test(execFileSync('ps', ['-o', 'stat=', '-p', String(pid)], {encoding: 'utf8'}).trim()); }
    catch { return false; }
  }
  try { process.kill(pid, 0); return true; } catch { return false; }
}

async function smoke(dir) {
  verifyChecksums(dir);
  const platform = platforms[process.platform], arch = architectures[process.arch];
  assert(platform && arch, 'Unsupported smoke runner');
  // Short paths matter for the supervisor's Unix socket.
  const root = fs.mkdtempSync(path.join(process.platform === 'win32' ? os.tmpdir() : '/tmp', 'b8-'));
  const extracted = path.join(root, 'archive'), cwd = path.join(root, 'outside-git'), home = path.join(root, 'user');
  for (const folder of [extracted, cwd, home, path.join(root, 'run')]) fs.mkdirSync(folder);
  const archive = path.join(dir, `bonsai_${platform}_${arch}.${platform === 'windows' ? 'zip' : 'tar.gz'}`);
  if (platform === 'windows') {
    execFileSync('powershell.exe', ['-NoProfile', '-Command', 'Expand-Archive -LiteralPath $env:BONSAI_SMOKE_ARCHIVE -DestinationPath $env:BONSAI_SMOKE_DEST'], {
      env: {...process.env, BONSAI_SMOKE_ARCHIVE: archive, BONSAI_SMOKE_DEST: extracted},
    });
  } else execFileSync('tar', ['-xzf', archive, '-C', extracted]);
  const binary = path.join(extracted, platform === 'windows' ? 'bonsai.exe' : 'bonsai');
  const env = {...process.env, HOME: home, USERPROFILE: home,
    XDG_CONFIG_HOME: path.join(home, 'config'), XDG_STATE_HOME: path.join(home, 'state'),
    XDG_CACHE_HOME: path.join(home, 'cache'), XDG_RUNTIME_DIR: path.join(root, 'run'),
    APPDATA: path.join(home, 'appdata'), AppData: path.join(home, 'appdata'),
    LOCALAPPDATA: path.join(home, 'localappdata'), LocalAppData: path.join(home, 'localappdata'),
    GIT_CONFIG_GLOBAL: path.join(home, 'gitconfig'), GIT_CONFIG_NOSYSTEM: '1',
    BONSAI_DAEMON_BIN: binary, BONSAI_FRONTEND_ORIGIN: 'https://retired.example'};
  const options = {cwd, env};
  const stateDir = platform === 'windows' ? env.LOCALAPPDATA : platform === 'darwin' ? path.join(home, 'Library', 'Application Support') : env.XDG_STATE_HOME;
  const daemonPidFile = path.join(stateDir, 'bonsai', 'web', '.bonsai', 'daemon.pid');
  const childPids = new Set();
  let started = false;
  try {
    assert.match(await run(binary, ['version'], options), /^bonsai /);
    const port = await freePort(), origin = `http://127.0.0.1:${port}`;
    const output = await run(binary, ['web', '--no-open', '--no-setup', '--port', String(port)], options);
    started = true;
    assert(output.includes(`${origin}/app`), output);
    assert(!output.includes('retired.example'), output);
    const status = await run(binary, ['web', 'status'], options);
    assert.match(status, /web · ready/);
    for (const match of status.matchAll(/^\s*(?:api|tunnel)\s+\S+\s+(\d+)/gm)) childPids.add(Number(match[1]));
    assert(childPids.size > 0, `No supervised PIDs found:\n${status}`);
    const get = (url, init = {}) => fetch(new URL(url, origin), {...init, redirect: 'manual', signal: AbortSignal.timeout(5000)});
    const rootPage = await get('/');
    assert.equal(rootPage.status, 302);
    assert.equal(rootPage.headers.get('location'), '/app/');
    const ui = await get('/app');
    assert.equal(ui.status, 200);
    assert.match(ui.headers.get('content-type'), /text\/html/);
    assert(ui.headers.get('content-security-policy').includes("connect-src 'self';"));
    const html = await ui.text();
    assert(html.includes('id="root"') && !html.includes('does not include the web UI'), 'Archive contains the placeholder UI');
    assert(html.includes(`name="bonsai-local-api-origin" content="${origin}"`));
    const assets = [...html.matchAll(/(?:src|href)="(\/assets\/[^"?#]+\.(?:js|css))"/g)].map(match => match[1]);
    assert(assets.some(file => file.endsWith('.js')) && assets.some(file => file.endsWith('.css')), 'Missing JS/CSS references');
    for (const file of assets) {
      const asset = await get(file);
      assert.equal(asset.status, 200, file);
      assert.match(asset.headers.get('content-type'), file.endsWith('.js') ? /javascript/ : /text\/css/);
      assert.equal(asset.headers.get('cache-control'), 'public, max-age=31536000, immutable');
      assert(!/__bonsaiTestStore|api\.bonsai\.dev|app\.bonsai\.dev|\/auth\/github/.test(await asset.text()), file);
    }
    for (const route of ['/app/github', '/app/settings', '/app/worktrees']) {
      const page = await get(route);
      assert.equal(page.status, 200, route);
      assert.equal(await page.text(), html, route);
    }
    for (const foreign of ['https://app.bonsai.dev', 'https://evil.example', `http://127.0.0.1:${port + 1}`]) {
      assert.equal((await get('/api/session', {method: 'POST', headers: {Origin: foreign}})).status, 403);
    }
    assert.equal((await get('/api/projects', {headers: {Origin: origin}})).status, 401);
    const session = await get('/api/session', {method: 'POST', headers: {Origin: origin}});
    assert.equal(session.status, 201);
    const capability = await session.json();
    assert(capability.token);
    const projects = await get('/api/projects', {headers: {Origin: origin, 'X-Bonsai-Session': capability.token}});
    assert.equal(projects.status, 200);
    assert.deepEqual(await projects.json(), []);
    assert.equal((await get('/version')).status, 200);
    assert.match(await run(binary, ['web', 'logs', 'api'], options), /local API|listening/i);
    await run(binary, ['web', 'stop'], options);
    started = false;
    await waitFor(() => [...childPids].every(pid => !alive(pid)), 'Supervised children survived bonsai web stop');
    await waitFor(async () => { try { await get('/health'); return false; } catch { return true; } }, 'API still listening after stop');
    console.log(`Archive smoke passed: ${platform}/${arch} (checksums, production UI, assets, deep links, capability, shutdown)`);
  } finally {
    if (started) await run(binary, ['web', 'stop'], options).catch(() => {});
    // An idle supervisor remains available for retained logs by design. Remove
    // this isolated test supervisor too, before deleting its user directories.
    if (fs.existsSync(daemonPidFile)) {
      const pid = Number(fs.readFileSync(daemonPidFile, 'utf8').split(/\r?\n/)[0]);
      if (alive(pid)) process.kill(pid, 'SIGTERM');
      await waitFor(() => !alive(pid), 'Isolated supervisor did not exit');
    }
    for (const pid of childPids) if (alive(pid)) process.kill(pid, 'SIGTERM');
    fs.rmSync(root, {recursive: true, force: true, maxRetries: 10, retryDelay: 100});
  }
}

module.exports = {verifyChecksums, expectedArchives};
if (require.main === module) smoke(path.resolve(process.argv[2] || 'dist')).catch(error => { console.error(error); process.exitCode = 1; });
