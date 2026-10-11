const assert = require('node:assert/strict');
const test = require('node:test');
const vm = require('node:vm');
const fs = require('node:fs');

function setup(realtime = 'true') {
 const requests = [], triggers = [], listeners = {};
 const elements = {
  'workflow-repo-filter': {value: '42'},
  'workflow-conclusion-filter': {value: 'failure'},
  'workflow-limit': {value: '50'},
  'workflows-container': {setAttribute(key, value) { this[key] = value; }},
 };
 let sockets = 0;
 const context = {console, URLSearchParams, setTimeout, clearTimeout,
  document: {getElementById(id) {return elements[id];}, addEventListener(name, fn) {listeners[name] = fn;},
   body: {dataset: {realtime}, addEventListener(name, fn) {listeners[name] = fn;}}},
  window: {location: {protocol: 'https:', host: 'example.com'}, addEventListener() {}},
  htmx: {ajax(method, url) {requests.push(url);}, trigger(selector, event) {assert.equal(event, 'refresh'); triggers.push(selector);}},
  WebSocket: function() {sockets++;},
 };
 vm.createContext(context);
 vm.runInContext(fs.readFileSync('kodata/static/dashboard.js', 'utf8'), context);
 return {context, requests, triggers, listeners, elements, sockets: () => sockets};
}

test('every supported realtime event preserves the active workflow filters', () => {
 for (const type of ['workflow_update', 'check_run_update', 'installation_update', 'repositories_added', 'repositories_removed']) {
  const state = setup();
  state.context.handleWebSocketMessage({type});
  assert.equal(state.requests[0], '/api/workflows?repository_id=42&conclusion=failure&limit=50');
  assert.equal(state.elements['workflows-container']['hx-get'], state.requests[0]);
  assert.deepEqual(state.triggers, ['#repositories-container', '#analytics-stats', '#flaky-tests-container']);
 }
});

test('HTMX HTML responses have no JSON beforeSwap hook', () => {
 assert.equal(setup().listeners['htmx:beforeSwap'], undefined);
});

test('disabled realtime creates no socket', () => {
 const state = setup('false');
 state.context.connectWebSocket();
 assert.equal(state.sockets(), 0);
});

test('unknown events do not refresh data', () => {
 const state = setup();
 state.context.handleWebSocketMessage({type: 'unknown'});
 assert.equal(state.requests.length, 0);
});
