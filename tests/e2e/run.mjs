#!/usr/bin/env node
// End-to-end test runner: builds the server, then for each suite starts the
// server(s) it needs (with its own configuration) against a dedicated Redis
// database, runs the suite script, and reports PASS/FAIL lines.
//
// Usage:   node tests/e2e/run.mjs [name-filter ...]
// Prereqs: Redis, redis-cli, Go, the built frontend (cd frontend && npm run build),
//          and for browser suites: cd tests/e2e && npm ci && npx playwright install
// Env:     REDIS_ADDR        host:port of Redis           (default localhost:6379)
//          E2E_REDIS_DB      database the tests may use   (default 15; must be empty)
//          E2E_PORT_BASE     first port for test servers  (default 18200)
//          E2E_SKIP_BROWSER  set to 1 to skip browser suites
import { execFileSync, execSync, spawn } from 'node:child_process';
import { existsSync, mkdirSync, rmSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(HERE, '../..');
const ARTIFACTS = join(HERE, 'artifacts');
const [redisHost, redisPort] = (process.env.REDIS_ADDR || 'localhost:6379').split(':');
const DB = process.env.E2E_REDIS_DB || '15';
const REDIS_CLI = `redis-cli -h ${redisHost} -p ${redisPort || 6379} -n ${DB}`;
let port = +(process.env.E2E_PORT_BASE || 18200);

const off = { RATE_LIMIT: 'false' }; // most suites create many rooms from one IP
const SUITES = [
  { name: 'chat', script: 'api/chat.mjs', env: off },
  { name: 'secret', script: 'api/secret.mjs', env: off, node: ['--no-warnings'] },
  { name: 'file', script: 'api/file.mjs', env: { ...off, HISTORY_FOR_NEW_MEMBERS: 'true' } },
  ...['200', '5', '0'].map(n => ({ name: `history-limit-${n}`, script: 'api/history.mjs', env: { ...off, HISTORY_FOR_NEW_MEMBERS: 'true', HISTORY_LIMIT: n } })),
  { name: 'file-store', script: 'api/filestore.mjs', env: { ...off, EMPTY_ROOM_TTL: '2s' } },
  { name: 'empty-room', script: 'api/empty.mjs', servers: 2, env: { ...off, EMPTY_ROOM_TTL: '2s', HISTORY_FOR_NEW_MEMBERS: 'true' } },
  { name: 'presence', script: 'api/presence.mjs', servers: 2, env: { ...off, HISTORY_FOR_NEW_MEMBERS: 'true' } },
  { name: 'presence-heal', script: 'api/heal.mjs', env: off },
  { name: 'shutdown-and-crash', script: 'api/ghost.mjs', servers: 0, env: {} },
  { name: 'admin-moderation', script: 'api/admin.mjs', env: off },
  { name: 'room-limit', script: 'api/roomlimit.mjs', env: { ...off, MAX_ROOMS: '3', EMPTY_ROOM_TTL: '2s' } },
  { name: 'hardening-http', script: 'api/hardening.mjs', env: off },
  { name: 'hardening-https', script: 'api/hardening.mjs', env: { ...off, PUBLIC_URL: 'https://chat.example.test' } },
  { name: 'ratelimit-trusted-proxy', script: 'api/ratelimit.mjs', args: ['trusted'], env: { TRUSTED_PROXIES: '127.0.0.1,::1' } },
  { name: 'ratelimit-spoofed-xff', script: 'api/ratelimit.mjs', args: ['untrusted'], env: {} },
  { name: 'ratelimit-disabled', script: 'api/ratelimit.mjs', args: ['disabled'], env: off },
  { name: 'file-storage-guard', script: 'api/ratelimit.mjs', args: ['filesfull'], env: { STORAGE_LIMIT_MB: '1024', FILE_STORAGE_LIMIT_MB: '1' } },
  { name: 'storage-guard', script: 'api/ratelimit.mjs', args: ['storagefull'], env: { STORAGE_LIMIT_MB: '1', FILE_STORAGE_LIMIT_MB: '0' },
    // Guarantee Redis uses more than 1 MB, even on a fresh instance.
    setup: () => execSync(`${REDIS_CLI} -x set e2e:filler`, { input: Buffer.alloc(2 << 20, 97) }) },

  { name: 'ui-chat', script: 'browser/ui.js', browser: true, env: off },
  { name: 'ui-files', script: 'browser/files-ui.js', browser: true, env: off },
  { name: 'ui-image-preview', script: 'browser/preview-ui.js', browser: true, env: off },
  { name: 'ui-history', script: 'browser/history-ui.js', browser: true, env: { ...off, HISTORY_FOR_NEW_MEMBERS: 'true' } },
  ...['false', 'true'].map(v => ({ name: `ui-new-members-${v}`, script: 'browser/newmember-ui.js', browser: true, env: { ...off, HISTORY_FOR_NEW_MEMBERS: v } })),
  { name: 'ui-presence', script: 'browser/presence-ui.js', browser: true, env: off },
  { name: 'ui-home-links', script: 'browser/home-links.js', browser: true, env: off },
  { name: 'ui-homepage', script: 'browser/home-shots.js', browser: true, env: off },
  { name: 'ui-mobile', script: 'browser/mobile-check.js', browser: true, env: off },
  { name: 'ui-rate-limits', script: 'browser/ratelimit-ui.js', browser: true, env: {} },
  { name: 'ui-moderation', script: 'browser/moderation-ui.js', browser: true, env: off },
  { name: 'ui-reconnect', script: 'browser/reconnect-ui.js', browser: true, servers: 0, env: {} },
];

const redis = cmd => execSync(`${REDIS_CLI} ${cmd}`).toString().trim();
const sleep = ms => new Promise(r => setTimeout(r, ms));

function preflight() {
  for (const f of ['frontend/dist/index.html', 'static/site.css']) {
    if (!existsSync(join(ROOT, f))) throw new Error(`${f} is missing — run: cd frontend && npm run build`);
  }
  if (redis('ping') !== 'PONG') throw new Error(`Redis not reachable with: ${REDIS_CLI}`);
  const size = +redis('dbsize');
  if (size !== 0) throw new Error(`Redis db ${DB} is not empty (${size} keys). The tests flush it, so point E2E_REDIS_DB at an unused database.`);
  mkdirSync(ARTIFACTS, { recursive: true });
}

async function startServer(bin, p, env) {
  const proc = spawn(bin, [], {
    cwd: ROOT,
    env: { ...process.env, ...env, SERVER_ADDR: `:${p}`, REDIS_ADDR: `${redisHost}:${redisPort || 6379}`, REDIS_DB: DB, GIN_MODE: 'release' },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  proc.log = '';
  proc.stdout.on('data', d => (proc.log += d));
  proc.stderr.on('data', d => (proc.log += d));
  for (let i = 0; i < 100; i++) {
    try { if ((await fetch(`http://localhost:${p}/robots.txt`)).ok) return proc; } catch {}
    if (proc.exitCode !== null) break;
    await sleep(100);
  }
  throw new Error(`server on :${p} did not start:\n${proc.log}`);
}

async function stopServer(proc) {
  if (proc.exitCode !== null) return;
  const exited = new Promise(r => proc.once('exit', r));
  proc.kill('SIGINT'); // graceful shutdown
  if (await Promise.race([exited.then(() => true), sleep(5000).then(() => false)])) return;
  proc.kill('SIGKILL');
  await exited;
}

function runScript(suite, env) {
  return new Promise(resolvePromise => {
    const child = spawn(process.execPath, [...(suite.node || []), join(HERE, suite.script), ...(suite.args || [])], { cwd: HERE, env });
    let out = '';
    child.stdout.on('data', d => (out += d));
    child.stderr.on('data', d => (out += d));
    const timer = setTimeout(() => { out += '\nFAIL suite timed out\n'; child.kill('SIGKILL'); }, 240_000);
    child.on('exit', code => { clearTimeout(timer); resolvePromise({ code, out }); });
  });
}

async function main() {
  const filters = process.argv.slice(2);
  const suites = SUITES.filter(s =>
    (!filters.length || filters.some(f => s.name.includes(f))) && !(s.browser && process.env.E2E_SKIP_BROWSER === '1'));
  preflight();

  const bin = join(ARTIFACTS, 'cloudchat-e2e-server');
  execFileSync('go', ['build', '-o', bin, './cmd/server'], { cwd: ROOT, stdio: 'inherit' });

  let failedSuites = 0, passes = 0, fails = 0;
  for (const s of suites) {
    redis('flushdb');
    // Each suite gets an empty file store, shared by its servers (like a
    // shared volume); files expire from it quickly so tests can see that.
    const fileDir = join(ARTIFACTS, 'files', s.name);
    rmSync(fileDir, { recursive: true, force: true });
    const env = { FILE_STORAGE_DIR: fileDir, FILE_SWEEP_INTERVAL: '1s', ...s.env };
    s.setup?.();
    const ports = [port, port + 1];
    port += 2;
    const servers = [];
    let result;
    try {
      for (let i = 0; i < (s.servers ?? 1); i++) servers.push(await startServer(bin, ports[i], env));
      result = await runScript(s, {
        ...process.env, ...env,
        BASE_URL: `http://localhost:${ports[0]}`, BASE_URL2: `http://localhost:${ports[1]}`,
        REDIS_CLI, REDIS_ADDR: `${redisHost}:${redisPort || 6379}`, REDIS_DB: DB,
        SERVER_BIN: bin, REPO_ROOT: ROOT, ARTIFACTS_DIR: ARTIFACTS,
      });
    } catch (e) {
      result = { code: 1, out: `FAIL ${e.message}\n` };
    } finally {
      for (const p of servers) await stopServer(p);
    }
    const lines = result.out.split('\n');
    const p = lines.filter(l => l.startsWith('PASS')).length;
    const f = lines.filter(l => l.startsWith('FAIL')).length;
    passes += p; fails += f;
    const ok = result.code === 0 && f === 0 && p > 0;
    if (!ok) {
      failedSuites++;
      writeFileSync(join(ARTIFACTS, `${s.name}.log`), result.out + '\n--- server logs ---\n' + servers.map(x => x.log).join('\n---\n'));
    }
    console.log(`${ok ? '✓' : '✗'} ${s.name.padEnd(26)} ${p} passed${f ? `, ${f} failed` : ''}${!ok && !f ? ` (exit ${result.code})` : ''}`);
    if (!ok) for (const l of lines.filter(l => l.startsWith('FAIL') || /Error|exception/.test(l)).slice(0, 8)) console.log(`    ${l}`);
  }
  redis('flushdb');

  console.log(`\n${suites.length - failedSuites}/${suites.length} suites passed (${passes} checks passed, ${fails} failed)`);
  if (failedSuites) console.log(`Logs for failed suites: ${ARTIFACTS}`);
  process.exit(failedSuites ? 1 : 0);
}

main().catch(e => { console.error(e.message); process.exit(1); });
