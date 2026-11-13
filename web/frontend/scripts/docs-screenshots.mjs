// Takes the console screenshots of the documentation site, in light and dark.
//
// Starts a MaxIOFS of its own on a temporary data directory, fills it with
// example data and captures each page with the frontend's Playwright.
//
//   make build                                                # binary with the current console
//   node web/frontend/scripts/docs-screenshots.mjs            # all screenshots
//   node web/frontend/scripts/docs-screenshots.mjs buckets    # only those whose name has "buckets"
//
// Options (environment): MAXIOFS_BINARY (default build/maxiofs[.exe]),
// DOCS_BROWSER_CHANNEL (default msedge; "chromium" for Playwright's own).

import { spawn, execSync } from 'node:child_process';
import crypto from 'node:crypto';
import { createRequire } from 'node:module';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..', '..');
const require = createRequire(path.join(repo, 'web', 'frontend', 'package.json'));
const { chromium } = require('playwright');

const out = path.join(repo, 'docs', 'assets', 'screenshots');
const binary = process.env.MAXIOFS_BINARY ||
  path.join(repo, 'build', process.platform === 'win32' ? 'maxiofs.exe' : 'maxiofs');
const s3Port = 18180;
const consolePort = 18181;
const consoleURL = `http://127.0.0.1:${consolePort}`;
const only = process.argv[2] || '';

const adminPassword = 'Docs-Admin-2026!';
const viewport = { width: 1440, height: 900 };

// ---------------------------------------------------------------- server

const dataDir = fs.mkdtempSync(path.join(os.tmpdir(), 'maxiofs-docs-'));
let server;

function startServer() {
  if (!fs.existsSync(binary)) {
    throw new Error(`no binary at ${binary}: run "make build" or set MAXIOFS_BINARY`);
  }
  server = spawn(binary, [
    '--data-dir', dataDir,
    '--listen', `127.0.0.1:${s3Port}`,
    '--console-listen', `127.0.0.1:${consolePort}`,
  ], { stdio: ['ignore', 'ignore', 'pipe'] });
  let log = '';
  server.stderr.on('data', (d) => { log = (log + d).slice(-4000); });
  server.on('exit', (code, signal) => {
    if (!stopping) {
      console.error(`maxiofs exited (${code ?? signal}) while the screenshots were taken:\n${log}`);
      process.exit(1);
    }
  });
}

let stopping = false;

function stopServer() {
  stopping = true;
  if (!server || server.exitCode !== null) return;
  if (process.platform === 'win32') {
    try { execSync(`taskkill /pid ${server.pid} /t /f`, { stdio: 'ignore' }); } catch { /* gone */ }
  } else {
    server.kill('SIGTERM');
  }
}

async function waitForConsole() {
  const until = Date.now() + 60_000;
  while (Date.now() < until) {
    try {
      const r = await fetch(`${consoleURL}/api/v1/health`);
      if (r.ok) return;
    } catch { /* not up yet */ }
    await new Promise((r) => setTimeout(r, 500));
  }
  throw new Error('the console did not start within 60 s');
}

// ---------------------------------------------------------------- API

async function api(method, p, token, body, contentType = 'application/json') {
  const headers = {};
  if (token) headers.Authorization = `Bearer ${token}`;
  if (body !== undefined) headers['Content-Type'] = contentType;
  const r = await fetch(`${consoleURL}/api/v1${p}`, {
    method,
    headers,
    body: body === undefined ? undefined : contentType === 'application/json' ? JSON.stringify(body) : body,
    signal: AbortSignal.timeout(60_000),
  });
  const text = await r.text();
  let json = {};
  try { json = JSON.parse(text); } catch { /* not JSON */ }
  if (!r.ok) throw new Error(`${method} ${p}: ${r.status} ${text.slice(0, 300)}`);
  return json.data ?? json;
}

async function login(username, password) {
  const d = await api('POST', '/auth/login', '', { username, password });
  return d.token || d.access_token;
}

// ---------------------------------------------------------------- S3

const sha256 = (data) => crypto.createHash('sha256').update(data).digest('hex');
const hmac = (key, data) => crypto.createHmac('sha256', key).update(data).digest();
const encodeKey = (p) => p.split('/').map((s) => encodeURIComponent(s).replace(/[!'()*]/g, (c) => '%' + c.charCodeAt(0).toString(16).toUpperCase())).join('/');

// s3 sends one request signed with Signature V4, as any S3 client would.
async function s3(keys, method, pathname, query = '', body = '') {
  const host = `127.0.0.1:${s3Port}`;
  const amzDate = new Date().toISOString().replace(/[:-]|\.\d{3}/g, '');
  const date = amzDate.slice(0, 8);
  const payload = sha256(body);
  const canonicalQuery = query.split('&').filter(Boolean).sort().join('&');
  const signed = 'host;x-amz-content-sha256;x-amz-date';
  const canonical = [method, encodeKey(pathname), canonicalQuery,
    `host:${host}\nx-amz-content-sha256:${payload}\nx-amz-date:${amzDate}\n`, signed, payload].join('\n');
  const scope = `${date}/us-east-1/s3/aws4_request`;
  const toSign = ['AWS4-HMAC-SHA256', amzDate, scope, sha256(canonical)].join('\n');
  let k = hmac('AWS4' + keys.secret, date);
  for (const part of ['us-east-1', 's3', 'aws4_request']) k = hmac(k, part);
  const signature = crypto.createHmac('sha256', k).update(toSign).digest('hex');
  const r = await fetch(`http://${host}${encodeKey(pathname)}${canonicalQuery ? '?' + canonicalQuery : ''}`, {
    method,
    headers: {
      'x-amz-date': amzDate,
      'x-amz-content-sha256': payload,
      Authorization: `AWS4-HMAC-SHA256 Credential=${keys.id}/${scope}, SignedHeaders=${signed}, Signature=${signature}`,
    },
    body: body === '' ? undefined : body,
    signal: AbortSignal.timeout(30_000),
  });
  await r.arrayBuffer();
  return r.status;
}

// traffic gives the metrics pages requests to show.
async function traffic(keys) {
  const statuses = {};
  const count = (s) => { statuses[s] = (statuses[s] || 0) + 1; };
  for (let round = 0; round < 6; round++) {
    count(await s3(keys, 'GET', '/'));
    for (const b of ['media', 'backups', 'app-logs', 'website']) count(await s3(keys, 'GET', `/${b}`, 'list-type=2'));
    for (const key of ['README.txt', 'documents/brand-guidelines.pdf', 'photos/2026/launch-event.jpg']) {
      count(await s3(keys, 'HEAD', `/media/${key}`));
      count(await s3(keys, 'GET', `/media/${key}`));
    }
    count(await s3(keys, 'PUT', `/app-logs/app/heartbeat-${round}.log`, '', `round ${round}\n`.repeat(200)));
    count(await s3(keys, 'GET', '/media/missing-object.txt'));
  }
  if (!statuses[200]) throw new Error(`S3 requests failed: ${JSON.stringify(statuses)}`);
}

const bytes = (n, seed = 1) => {
  const b = Buffer.alloc(n);
  for (let i = 0; i < n; i++) b[i] = (i * 31 + seed * 17) & 0xff;
  return b;
};

async function put(token, bucket, key, body, type) {
  await api('PUT', `/buckets/${bucket}/objects/${encodeURIComponent(key)}`, token, body, type);
}

// seed fills the server with what a small company would have after a while.
async function seed(token) {
  await api('POST', '/buckets', token, { name: 'media', region: 'us-east-1', versioning: { status: 'Enabled' } });
  await api('POST', '/buckets', token, {
    name: 'backups', region: 'us-east-1', versioning: { status: 'Enabled' },
    objectLock: { enabled: true, mode: 'GOVERNANCE', days: 30 },
  });
  await api('POST', '/buckets', token, { name: 'app-logs', region: 'us-east-1' });
  await api('POST', '/buckets', token, { name: 'website', region: 'us-east-1' });

  const media = [
    ['photos/2026/launch-event.jpg', 2_400_000, 'image/jpeg'],
    ['photos/2026/team-offsite.jpg', 1_850_000, 'image/jpeg'],
    ['photos/2026/office-tour.jpg', 3_100_000, 'image/jpeg'],
    ['videos/product-demo.mp4', 9_600_000, 'video/mp4'],
    ['documents/annual-report-2025.pdf', 1_200_000, 'application/pdf'],
    ['documents/brand-guidelines.pdf', 640_000, 'application/pdf'],
    ['README.txt', 1_400, 'text/plain'],
  ];
  for (const [key, size, type] of media) await put(token, 'media', key, bytes(size, key.length), type);

  for (let v = 1; v <= 3; v++) {
    await put(token, 'backups', 'veeam/job-daily/2026-11-12.vbk', bytes(4_000_000 + v * 1000, v), 'application/octet-stream');
  }
  await put(token, 'backups', 'veeam/job-daily/2026-11-11.vbk', bytes(3_900_000, 9), 'application/octet-stream');
  await put(token, 'backups', 'databases/postgres-2026-11-12.sql.gz', bytes(820_000, 4), 'application/gzip');

  for (const day of ['2026-11-10', '2026-11-11', '2026-11-12']) {
    await put(token, 'app-logs', `app/${day}.log`, Buffer.from(`${day} INFO started\n`.repeat(4000)), 'text/plain');
  }
  await api('PUT', '/buckets/app-logs/lifecycle', token,
    '<LifecycleConfiguration>' +
    '<Rule><ID>expire-app-logs</ID><Status>Enabled</Status><Filter><Prefix>app/</Prefix></Filter><Expiration><Days>90</Days></Expiration></Rule>' +
    '<Rule><ID>abort-incomplete-uploads</ID><Status>Enabled</Status><Filter><Prefix></Prefix></Filter><AbortIncompleteMultipartUpload><DaysAfterInitiation>7</DaysAfterInitiation></AbortIncompleteMultipartUpload></Rule>' +
    '</LifecycleConfiguration>', 'application/xml');

  await put(token, 'website', 'index.html', Buffer.from('<h1>Hello from MaxIOFS</h1>'), 'text/html');
  await put(token, 'website', 'error.html', Buffer.from('<h1>Not found</h1>'), 'text/html');

  // A share is signed with one of its creator's keys.
  const created = await api('POST', '/users/admin/access-keys', token);
  const keys = {
    id: created.access_key_id || created.accessKeyId || created.accessKey || created.id,
    secret: created.secret_access_key || created.secretAccessKey || created.secretKey || created.secret,
  };
  if (!keys.id || !keys.secret) throw new Error(`unexpected access key answer: ${Object.keys(created)}`);
  await api('POST', '/buckets/media/objects/' + encodeURIComponent('documents/annual-report-2025.pdf') + '/share', token, { expiresIn: 7 * 86400 });

  const tenant = await api('POST', '/tenants', token, {
    name: 'acme', displayName: 'Acme Corp', description: 'Customer tenant',
    maxStorageBytes: 500 * 1024 ** 3, maxBuckets: 20, maxAccessKeys: 50,
  });
  const alice = await api('POST', '/users', token, { username: 'alice', email: 'alice@example.com', password: 'Alice-pass-2026!', roles: ['user'] });
  const bob = await api('POST', '/users', token, { username: 'bob', email: 'bob@example.com', password: 'Bob-pass-2026!', roles: ['user'] });
  await api('POST', '/users', token, { username: 'carol', email: 'carol@acme.example', password: 'Carol-pass-2026!', roles: ['admin'], tenantId: tenant.id });
  await api('POST', '/users', token, { username: 'veeam', password: 'Veeam-pass-2026!', roles: ['user'] });

  const group = await api('POST', '/groups', token, { name: 'engineering', displayName: 'Engineering', description: 'Platform and backend teams' });
  await api('POST', `/groups/${group.id}/members`, token, { userId: alice.id });
  await api('POST', `/groups/${group.id}/members`, token, { userId: bob.id });

  await api('POST', `/users/${alice.id}/access-keys`, token);

  const carol = await login('carol', 'Carol-pass-2026!');
  await api('POST', '/buckets', carol, { name: 'acme-invoices', region: 'us-east-1' });
  await put(carol, 'acme-invoices', '2026/11/INV-10421.pdf', bytes(180_000, 3), 'application/pdf');

  await traffic(keys);
  return { alice, group };
}

// ---------------------------------------------------------------- shots

// settle waits for the page's data: no spinner, then a moment for charts.
async function settle(page) {
  await page.waitForLoadState('domcontentloaded');
  await page.waitForFunction(() => !document.querySelector('.animate-spin'), null, { timeout: 15_000 }).catch(() => {});
  await page.waitForTimeout(900);
}

async function clickText(page, text) {
  await page.getByText(text, { exact: true }).first().click();
  await settle(page);
}

function shots(ids) {
  const go = (route) => async (page) => { await page.goto(consoleURL + route); await settle(page); };
  // then opens a page and clicks its way to what the screenshot shows.
  const then = (route, ...labels) => async (page) => {
    await go(route)(page);
    for (const label of labels) await clickText(page, label);
  };
  const button = (route, name) => async (page) => {
    await go(route)(page);
    await page.getByRole('button', { name }).first().click();
    await settle(page);
  };
  const tab = then;
  const list = [
    ['dashboard', go('/')],
    ['buckets', go('/buckets')],
    ['bucket-create', go('/buckets/create')],
    ['bucket-create-object-lock', tab('/buckets/create', 'Object Lock & WORM')],
    ['bucket-create-access', tab('/buckets/create', 'Access Control')],
    ['bucket-objects', go('/buckets/media')],
    ['bucket-objects-folder', then('/buckets/media', 'photos', '2026')],
    ['bucket-upload', button('/buckets/media', 'Upload Files')],
    ['bucket-versions', then('/buckets/backups', 'veeam', 'job-daily', 'Show versions')],
    ['bucket-object-lock', go('/buckets/backups')],
    ['bucket-settings-general', go('/buckets/media/settings')],
    ['bucket-settings-security', tab('/buckets/media/settings', 'Security & Access')],
    ['bucket-settings-lifecycle', tab('/buckets/app-logs/settings', 'Lifecycle')],
    ['bucket-settings-notifications', tab('/buckets/media/settings', 'Notifications')],
    ['bucket-settings-replication', tab('/buckets/media/settings', 'Replication')],
    ['bucket-settings-inventory', tab('/buckets/media/settings', 'Inventory')],
    ['bucket-settings-website', tab('/buckets/website/settings', 'Static Website')],
    ['bucket-settings-quota', tab('/buckets/media/settings', 'Quota')],
    ['users', go('/users')],
    ['user-create', button('/users', 'Create User')],
    ['user-detail', go(`/users/${ids.alice.id}`)],
    ['access-keys', go('/users/access-keys')],
    ['groups', go('/groups')],
    ['group-create', button('/groups', 'New Group')],
    ['group-detail', go(`/groups/${ids.group.id}`)],
    ['tenants', go('/tenants')],
    ['tenant-create', button('/tenants', 'Create Tenant')],
    ['iam', async (page) => {
      await go('/iam')(page);
      const select = page.locator('select').first();
      const options = await select.locator('option').allTextContents();
      await select.selectOption({ index: options.findIndex((o) => o.includes('alice')) });
      await settle(page);
    }],
    ['iam-policies', tab('/iam', 'Policies')],
    ['identity-providers', go('/identity-providers')],
    ['identity-provider-create', button('/identity-providers', 'Add Provider')],
    ['security', go('/security')],
    ['audit-logs', go('/audit-logs')],
    ['metrics', async (page) => {
      await go('/metrics')(page);
      await page.getByText('Metrics Dashboard').waitFor({ timeout: 60_000 });
      await settle(page);
    }],
    ['metrics-storage', tab('/metrics', 'Storage')],
    ['metrics-api', tab('/metrics', 'API & Requests')],
    ['settings', go('/settings')],
    ['settings-email', tab('/settings', 'Email')],
    ['cluster', go('/cluster')],
    ['profile', go('/profile')],
    ['two-factor-setup', button('/profile', 'Enable 2FA')],
    ['about', go('/about')],
  ];
  return list.filter(([name]) => name.includes(only));
}

async function signIn(page, password) {
  await page.goto(consoleURL + '/login');
  await page.waitForSelector('input[type="password"]');
  await page.locator('input:not([type="password"])').first().fill('admin');
  await page.locator('input[type="password"]').fill(password);
  await page.locator('button[type="submit"]').click();
  await page.waitForURL((u) => !u.pathname.startsWith('/login'), { timeout: 20_000 });
  await settle(page);
}

async function capture(page, name, theme) {
  const file = path.join(out, `${name}-${theme}.png`);
  await page.screenshot({ path: file });
  console.log(`  ${path.relative(repo, file)}`);
}

async function main() {
  fs.mkdirSync(out, { recursive: true });
  startServer();
  await waitForConsole();

  let token = await login('admin', 'admin');
  try {
    browser = await chromium.launch({ channel: process.env.DOCS_BROWSER_CHANNEL || 'msedge', headless: true });
  } catch {
    browser = await chromium.launch({ headless: true });
  }
  const themes = ['light', 'dark'];
  const contexts = {};
  for (const theme of themes) {
    contexts[theme] = await browser.newContext({ viewport, colorScheme: theme, locale: 'en-US' });
  }

  // Before anything else: the sign-in page and the first sign-in.
  if ('login'.includes(only) || 'first-sign-in'.includes(only)) {
    for (const theme of themes) {
      const page = await contexts[theme].newPage();
      await page.goto(consoleURL + '/login');
      await settle(page);
      await capture(page, 'login', theme);
      await signIn(page, 'admin');
      await capture(page, 'first-sign-in', theme);
      await page.close();
    }
  }

  console.log('seeding example data');
  await api('PUT', '/users/admin/password', token, { currentPassword: 'admin', newPassword: adminPassword });
  token = await login('admin', adminPassword);
  // The recovery bundle taken, the console stops asking for it.
  await api('POST', '/settings/encryption/recovery-bundle', token, { passphrase: 'docs-recovery-passphrase' });
  const ids = await seed(token);
  console.log('seeded');

  for (const theme of themes) {
    const page = await contexts[theme].newPage();
    await signIn(page, adminPassword);
    for (const [name, show] of shots(ids)) {
      try {
        await show(page);
        await capture(page, name, theme);
      } catch (err) {
        console.error(`  ${name}-${theme}: ${err.message.split('\n')[0]}`);
      }
    }
    await page.close();
  }
}

let browser;

main()
  .catch((err) => { console.error(err.message); process.exitCode = 1; })
  .finally(async () => {
    await browser?.close().catch(() => {});
    stopServer();
    // The server lets go of its files a moment after it stops.
    setTimeout(() => fs.rmSync(dataDir, { recursive: true, force: true, maxRetries: 10, retryDelay: 300 }), 1000);
  });
