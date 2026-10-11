const {test: base, expect} = require('@playwright/test');
const {createHmac} = require('node:crypto');
const test = base.extend({
 page: async ({page}, use) => {
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('console', message => {if (message.type() === 'error') errors.push(message.text());});
  await use(page);
  expect(errors).toEqual([]);
 },
});

async function openDashboard(page) {
 await page.goto('/');
 await expect(page.locator('#repositories-container a')).toHaveCount(3);
 await expect(page.locator('#workflow-repo-filter option')).toHaveCount(4);
 await expect(page.locator('#flaky-count')).toHaveText(/^\d+$/);
}

async function webhook(request, type, event) {
 const data = JSON.stringify(event);
 const signature = createHmac('sha256', 'e2e-only-secret').update(data).digest('hex');
 const response = await request.post('/api/github/hook', {data, headers: {
  'content-type': 'application/json',
  'x-github-event': type,
  'x-github-delivery': 'e2e-' + Date.now(),
  'x-hub-signature-256': 'sha256=' + signature,
 }});
 expect(response.ok()).toBeTruthy();
}

test('dashboard renders HTML partials and accurate summary cards without errors', async ({page, request}) => {
 await openDashboard(page);
 const flaky = await (await request.get('/api/flaky-tests?days=30&min_runs=5')).json();
 const analytics = await (await request.get('/api/analytics?days=7')).json();
 await expect(page.locator('#flaky-count')).toHaveText(String(flaky.flaky_tests.length));
 await expect(page.locator('#trend-indicator')).toHaveText(analytics.success_rate.toFixed(1) + '%');
 await expect(page.locator('#workflows-container tbody tr')).not.toHaveCount(0);
});

test('combined workflow filters support empty results', async ({page}) => {
 await openDashboard(page);
 await page.locator('#workflow-repo-filter').selectOption('910003');
 await page.locator('#workflow-conclusion-filter').selectOption('cancelled');
 await expect(page.locator('#workflows-container')).toContainText('No workflow runs found.');
 await page.locator('#workflow-conclusion-filter').selectOption('failure');
 await expect(page.locator('#workflows-container tbody tr')).toHaveCount(1);
 await expect(page.locator('#workflows-container')).toContainText('Terraform Validate');
});

test('repository navigation and matrix filters retain state in card view', async ({page}) => {
 await openDashboard(page);
 await page.locator('#repositories-container').getByRole('link', {name:'acme-demo/payments-api'}).click();
 await expect(page.getByRole('table')).toBeVisible();
 for (const [name, key, value] of [['Workflow','workflow','Build and Test'],['Branch','branch','main'],['Event','event','push'],['View','view','cards']]) {
  await Promise.all([page.waitForURL(url => url.searchParams.get(key) === value), page.getByRole('combobox', {name, exact:true}).selectOption(value)]);
 }
 await expect(page.getByRole('combobox', {name:'Workflow', exact:true})).toHaveValue('Build and Test');
 await expect(page.getByRole('combobox', {name:'Branch', exact:true})).toHaveValue('main');
 await expect(page.getByRole('combobox', {name:'Event', exact:true})).toHaveValue('push');
 await expect(page.locator('main section h3')).toHaveText('Build and Test');
 await expect(page.getByRole('table')).toHaveCount(0);
});

test('a signed workflow event appears through realtime without losing filters', async ({page, request}) => {
 await openDashboard(page);
 await page.locator('#workflow-repo-filter').selectOption('910001');
 await page.locator('#workflow-conclusion-filter').selectOption('failure');
 await page.locator('#workflow-limit').selectOption('50');
 const now = new Date().toISOString();
 await webhook(request, 'workflow_run', {
  action:'completed', installation:{id:700001},
  repository:{id:910001, name:'payments-api', full_name:'acme-demo/payments-api', default_branch:'main', owner:{login:'acme-demo'}},
  workflow_run:{id:999001, workflow_id:999100, name:'E2E realtime failure', status:'completed', conclusion:'failure', head_branch:'main', head_sha:'abcdef0123456789abcdef0123456789abcdef01', event:'push', run_number:999,
   created_at:now, updated_at:now, run_started_at:now, html_url:'https://github.com/acme-demo/payments-api/actions/runs/999001'},
 });
 await expect(page.locator('#workflows-container')).toContainText('E2E realtime failure');
 await expect(page.locator('#workflow-repo-filter')).toHaveValue('910001');
 await expect(page.locator('#workflow-conclusion-filter')).toHaveValue('failure');
 await expect(page.locator('#workflow-limit')).toHaveValue('50');
});

test('a signed check completion refreshes the flaky card without a page reload', async ({page, request}) => {
 await openDashboard(page);
 const before = Number(await page.locator('#flaky-count').textContent());
 const now = new Date().toISOString();
 for (let i = 0; i < 5; i++) {
  await webhook(request, 'check_run', {
   action:'completed', installation:{id:700002},
   repository:{id:910002, name:'web-portal', full_name:'acme-demo/web-portal', default_branch:'main', owner:{login:'acme-demo'}},
   check_run:{id:999200+i,name:'E2E flaky check',status:'completed',conclusion:i===0?'failure':'success',started_at:now,completed_at:now},
  });
 }
 await expect(page.locator('#flaky-count')).toHaveText(String(before+1));
 await expect(page.locator('#flaky-tests-container')).toContainText('E2E flaky check');
});
