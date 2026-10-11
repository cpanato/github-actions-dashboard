const {Client} = require('pg');
const {spawn, spawnSync} = require('node:child_process');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const {randomBytes} = require('node:crypto');

(async () => {
 const raw = process.env.DASHBOARD_TEST_DATABASE_URL;
 if (!raw) throw new Error('Set DASHBOARD_TEST_DATABASE_URL to a disposable loopback PostgreSQL database ending in _test');
 const url = new URL(raw);
 if (!['postgres:', 'postgresql:'].includes(url.protocol) || !['localhost', '127.0.0.1'].includes(url.hostname) || !url.pathname.endsWith('_test')) {
  throw new Error('E2E requires a loopback PostgreSQL test database ending in _test');
 }
 const client = new Client({connectionString: raw});
 await client.connect();
 const schema = 'dashboard_e2e_' + randomBytes(12).toString('hex');
 await client.query(`CREATE SCHEMA ${schema}`);
 const temporary = await fs.mkdtemp(path.join(os.tmpdir(), 'dashboard-e2e-'));
 let child;
 let cleaned = false;
 async function cleanup() {
  if (cleaned) return;
  cleaned = true;
  try { await client.query(`DROP SCHEMA ${schema} CASCADE`); }
  finally { await client.end(); await fs.rm(temporary, {recursive: true, force: true}); }
 }
 try {
  url.searchParams.set('search_path', schema);
  const config = path.join(temporary, 'config.json');
  await fs.writeFile(config, JSON.stringify({
   demo_mode: true,
   server: {address: '127.0.0.1', port: 18080},
   database: {driver: 'postgres', url: url.toString()},
   github: {app: {webhook_secret: 'e2e-only-secret'}},
   options: {default_time_window: '168h', enable_realtime: true},
   logging: {level: 'error'},
  }));
  const binary = path.join(temporary, 'dashboard');
  const build = spawnSync('go', ['build', '-o', binary, '.'], {stdio: 'inherit'});
  if (build.status !== 0) throw new Error('Dashboard build failed');
  const env = {...process.env};
  // External developer credentials must not affect the synthetic test profile.
  for (const key of Object.keys(env)) if (key.startsWith('DASHBOARD_')) delete env[key];
  child = spawn(binary, ['-config', config], {stdio: 'inherit', env});
  process.once('SIGTERM', () => child.kill('SIGTERM'));
  process.once('SIGINT', () => child.kill('SIGINT'));
  child.once('exit', async code => { await cleanup(); process.exit(code || 0); });
 } catch (error) { if (child) child.kill('SIGTERM'); await cleanup(); throw error; }
})().catch(error => { console.error(error); process.exit(1); });
