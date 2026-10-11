const {test, expect} = require('@playwright/test');
const {createHmac} = require('node:crypto');

async function open(page) {
 await page.goto('/');
 await expect(page.locator('#workflow-repo-filter option')).toHaveCount(4);
 await expect(page.locator('#workflows-container tbody tr')).not.toHaveCount(0);
 await page.waitForFunction(() => ws && ws.readyState === WebSocket.OPEN);
}

async function webhook(request, event) {
 const data = JSON.stringify(event);
 const response = await request.post('/api/github/hook', {data, headers:{
  'content-type':'application/json', 'x-github-event':'installation_repositories',
  'x-github-delivery':'failure-e2e-'+Date.now(),
  'x-hub-signature-256':'sha256='+createHmac('sha256','e2e-only-secret').update(data).digest('hex'),
 }});
 expect(response.ok()).toBeTruthy();
}

test('reconnect refreshes data and preserves selected filters', async ({page}) => {
 const sockets=[];
 page.on('websocket',socket=>sockets.push(socket));
 await open(page);
 await page.locator('#workflow-repo-filter').selectOption('910003');
 await page.locator('#workflow-conclusion-filter').selectOption('failure');
 await expect(page.locator('#workflows-container')).toContainText('Terraform Validate');
 const refreshed=page.waitForResponse(response=>response.url().includes('/api/workflows?repository_id=910003&conclusion=failure') && response.ok());
 await page.evaluate(()=>ws.close());
 await expect.poll(()=>sockets.length).toBe(2);
 await page.waitForFunction(()=>ws.readyState===WebSocket.OPEN);
 await refreshed;
 await expect(page.locator('#workflow-repo-filter')).toHaveValue('910003');
 await expect(page.locator('#workflow-conclusion-filter')).toHaveValue('failure');
 await expect(page.locator('#workflows-container')).toContainText('Terraform Validate');
});

test('repository removal refreshes the list and selected repository', async ({page,request}) => {
 await open(page);
 const repository={id:97000,name:'temporary',full_name:'e2e/temporary',owner:{login:'e2e'},default_branch:'main'};
 const removed={action:'removed',installation:{id:97001},repositories_removed:[repository]};
 try {
  await webhook(request,{action:'added',installation:{id:97001},repositories_added:[repository]});
  await expect(page.locator('#workflow-repo-filter option[value="97000"]')).toHaveCount(1);
  await page.locator('#workflow-repo-filter').selectOption('97000');
  await expect(page.locator('#workflows-container')).toContainText('No workflow runs found.');
  await webhook(request,removed);
  await expect(page.locator('#repositories-container').getByRole('link',{name:'e2e/temporary'})).toHaveCount(0);
  await expect(page.locator('#workflow-repo-filter option[value="97000"]')).toHaveCount(0);
  await expect(page.locator('#workflow-repo-filter')).toHaveValue('');
  await expect(page.locator('#workflows-container tbody tr')).not.toHaveCount(0);
 } finally { await webhook(request,removed); }
});

test('database outage preserves rendered data and recovers after reconnect', async ({page,request}) => {
 const pageErrors=[];
 page.on('pageerror',error=>pageErrors.push(error.message));
 await open(page);
 await page.locator('#workflow-repo-filter').selectOption('910003');
 await page.locator('#workflow-conclusion-filter').selectOption('failure');
 await expect(page.locator('#workflows-container')).toContainText('Terraform Validate');
 const before=await page.locator('#workflows-container').textContent();
 await page.evaluate(()=>{window.failedResponses=0;document.body.addEventListener('htmx:responseError',()=>window.failedResponses++);});
 try {
  expect((await request.post('http://127.0.0.1:18081/database/off')).ok()).toBeTruthy();
  await expect.poll(async()=>(await request.get('/api/health')).status()).toBe(503);
  await page.evaluate(()=>refreshDashboard());
  await expect.poll(()=>page.evaluate(()=>window.failedResponses)).toBeGreaterThan(0);
  await expect(page.locator('#workflows-container')).toHaveText(before);
 } finally {
  expect((await request.post('http://127.0.0.1:18081/database/on')).ok()).toBeTruthy();
 }
 await expect.poll(async()=>(await request.get('/api/health')).status()).toBe(200);
 const recovered=page.waitForResponse(response=>response.url().includes('/api/workflows?repository_id=910003&conclusion=failure') && response.ok());
 await page.evaluate(()=>refreshDashboard());
 await recovered;
 await expect(page.locator('#workflows-container')).toContainText('Terraform Validate');
 await expect(page.locator('#workflow-repo-filter')).toHaveValue('910003');
 expect(pageErrors).toEqual([]);
});
