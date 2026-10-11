// HTMX owns HTML rendering. JSON is used only to populate filter options.
let ws = null;
let reconnectAttempts = 0;
let reconnectTimer = null;
let stopping = false;

async function populateRepoFilter() {
 const response = await fetch('/api/repositories');
 if (!response.ok) throw new Error('Failed to load repository filters');
 const data = await response.json();
 const select = document.getElementById('workflow-repo-filter');
 const selected = select.value;
 select.replaceChildren(new Option('All repositories', ''));
 for (const repo of data.repositories || []) {
  select.add(new Option(repo.full_name, String(repo.id)));
 }
 select.value = selected;
 if (!select.value) select.value = '';
}

function updateWorkflowFilters() {
 const params = new URLSearchParams();
 const repo = document.getElementById('workflow-repo-filter').value;
 const conclusion = document.getElementById('workflow-conclusion-filter').value;
 const limit = document.getElementById('workflow-limit').value;
 if (repo) params.set('repository_id', repo);
 if (conclusion) params.set('conclusion', conclusion);
 if (limit) params.set('limit', limit);
 const url = '/api/workflows?' + params.toString();
 document.getElementById('workflows-container').setAttribute('hx-get', url);
 return htmx.ajax('GET', url, {target: '#workflows-container', swap: 'innerHTML'});
}

function refreshDashboard() {
 for (const selector of ['#repositories-container', '#analytics-stats', '#flaky-tests-container']) {
  htmx.trigger(selector, 'load');
 }
 updateWorkflowFilters();
}

function handleWebSocketMessage(message) {
 if (['workflow_update', 'check_run_update', 'installation_update', 'repositories_added', 'repositories_removed'].includes(message.type)) {
  refreshDashboard();
 }
}

function connectWebSocket() {
 if (stopping || document.body.dataset.realtime !== 'true') return;
 const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
 ws = new WebSocket(`${protocol}//${window.location.host}/ws`);
 ws.onopen = () => { reconnectAttempts = 0; refreshDashboard(); };
 ws.onmessage = event => {
  try { handleWebSocketMessage(JSON.parse(event.data)); }
  catch (error) { console.error('Invalid realtime message', error); }
 };
 ws.onclose = () => {
  if (!stopping && reconnectAttempts < 5) {
   reconnectTimer = setTimeout(connectWebSocket, 2000 * ++reconnectAttempts);
  }
 };
}

document.addEventListener('DOMContentLoaded', () => {
 populateRepoFilter().catch(error => console.error(error));
 connectWebSocket();
});

document.body.addEventListener('htmx:afterSwap', event => {
 if (event.detail.target.id === 'repositories-container') {
  populateRepoFilter().then(updateWorkflowFilters).catch(error => console.error(error));
 }
});

window.addEventListener('pagehide', () => {
 stopping = true;
 clearTimeout(reconnectTimer);
 if (ws) ws.close();
});
