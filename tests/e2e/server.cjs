const {Client} = require('pg');
const {spawn, spawnSync} = require('node:child_process');
const fs = require('node:fs/promises');
const os = require('node:os');
const path = require('node:path');
const net = require('node:net');
const http = require('node:http');
const {randomBytes} = require('node:crypto');

(async () => {
 const raw = process.env.DASHBOARD_TEST_DATABASE_URL;
 if (!raw) throw new Error('Set DASHBOARD_TEST_DATABASE_URL to a disposable loopback PostgreSQL database ending in _test');
 const url = new URL(raw);
 if (!['postgres:', 'postgresql:'].includes(url.protocol) || !['localhost', '127.0.0.1'].includes(url.hostname) || !url.pathname.endsWith('_test')) {
  throw new Error('E2E requires a loopback PostgreSQL test database ending in _test');
 }
 const polling = process.argv.includes('--polling');
 const appPort = polling ? 18082 : 18080;
 const controlPort = appPort + 1;
 const client = new Client({connectionString: raw});
 await client.connect();
 const schema = 'dashboard_e2e_' + randomBytes(12).toString('hex');
 await client.query(`CREATE SCHEMA ${schema}`);
 const temporary = await fs.mkdtemp(path.join(os.tmpdir(), 'dashboard-e2e-'));
 let child;
 let databaseAvailable = true;
 const sockets = new Set();
 const proxy = net.createServer(socket => {
  if (!databaseAvailable) { socket.destroy(); return; }
  const upstream = net.connect({host:url.hostname, port:Number(url.port || 5432)});
  for (const connection of [socket, upstream]) {
   sockets.add(connection);
   connection.on('error', () => connection.destroy());
   connection.on('close', () => { sockets.delete(connection); socket.destroy(); upstream.destroy(); });
  }
  socket.pipe(upstream); upstream.pipe(socket);
 });
 const control = http.createServer((req, res) => {
  if (req.method !== 'POST' || !['/database/off', '/database/on'].includes(req.url)) {
   res.writeHead(404).end(); return;
  }
  databaseAvailable = req.url.endsWith('/on');
  if (!databaseAvailable) for (const socket of sockets) socket.destroy();
  res.writeHead(204).end();
 });
 let cleaned = false;
 async function cleanup() {
  if (cleaned) return;
  cleaned = true;
  for (const socket of sockets) socket.destroy();
  await Promise.all([new Promise(resolve => proxy.close(resolve)), new Promise(resolve => control.close(resolve))]);
  try { await client.query(`DROP SCHEMA ${schema} CASCADE`); }
  finally { await client.end(); await fs.rm(temporary, {recursive: true, force: true}); }
 }
 async function listen(server, port) {
  await new Promise((resolve, reject) => {
   server.once('error', reject);
   server.listen(port, '127.0.0.1', () => { server.removeListener('error', reject); resolve(); });
  });
 }
 try {
  await listen(proxy, 0);
  await listen(control, controlPort);
  const appURL = new URL(url);
  appURL.port = String(proxy.address().port);
  appURL.searchParams.set('search_path', schema);
  const config = path.join(temporary, 'config.json');
  await fs.writeFile(config, JSON.stringify({
   demo_mode: true,
   server: {address: '127.0.0.1', port: appPort},
   database: {driver: 'postgres', url: appURL.toString()},
   github: {app: {webhook_secret: 'e2e-only-secret'}},
   options: {default_time_window: '168h', enable_realtime: !polling},
   // Tests assert immediate refreshes after signed webhooks, so disable stats caching.
   cache: {stats_ttl: '-1s'},
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
