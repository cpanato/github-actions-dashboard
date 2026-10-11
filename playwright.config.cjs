const {defineConfig} = require('@playwright/test');
module.exports = defineConfig({
 testDir: './tests/e2e',
 workers: 1,
 fullyParallel: false,
 timeout: 30000,
 reporter: [['list'], ['html', {open: 'never'}]],
 use: {baseURL: 'http://127.0.0.1:18080', trace: 'retain-on-failure', screenshot: 'only-on-failure'},
 projects: [
  {name:'realtime', testIgnore:'**/realtime-disabled.spec.cjs'},
  {name:'polling', testMatch:'**/realtime-disabled.spec.cjs', use:{baseURL:'http://127.0.0.1:18082'}},
 ],
 webServer: [{
  command: 'node tests/e2e/server.cjs',
  url: 'http://127.0.0.1:18080/api/health',
  reuseExistingServer: false,
  timeout: 120000,
  gracefulShutdown: {signal: 'SIGTERM', timeout: 15000},
 }, {
  command:'node tests/e2e/server.cjs --polling',
  url:'http://127.0.0.1:18082/api/health',
  reuseExistingServer:false,
  timeout:120000,
  gracefulShutdown:{signal:'SIGTERM',timeout:15000},
 }],
});
