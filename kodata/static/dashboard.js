// WebSocket connection for real-time updates
let ws = null;
let reconnectAttempts = 0;
const maxReconnectAttempts = 5;

function connectWebSocket() {
	const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
	const wsUrl = `${protocol}//${window.location.host}/ws`;

	ws = new WebSocket(wsUrl);

	ws.onopen = function() {
		console.log('WebSocket connected');
		reconnectAttempts = 0;
		// Refresh data on connection
		htmx.trigger('#repositories-container', 'load');
		htmx.trigger('#workflows-container', 'load');
		htmx.trigger('#analytics-stats', 'load');
		htmx.trigger('#flaky-tests-container', 'load');
	};

	ws.onmessage = function(event) {
		const message = JSON.parse(event.data);
		handleWebSocketMessage(message);
	};

	ws.onclose = function() {
		console.log('WebSocket disconnected');
		// Try to reconnect
		if (reconnectAttempts < maxReconnectAttempts) {
			reconnectAttempts++;
			setTimeout(connectWebSocket, 2000 * reconnectAttempts);
		}
	};

	ws.onerror = function(error) {
		console.error('WebSocket error:', error);
	};
}

function handleWebSocketMessage(message) {
	console.log('WebSocket message:', message);

	if (message.type === 'workflow_update' || message.type === 'check_run') {
		// Refresh repositories (to update stats)
		htmx.trigger('#repositories-container', 'load');
		// Refresh workflows list
		htmx.trigger('#workflows-container', 'load');
		// Refresh analytics
		htmx.trigger('#analytics-stats', 'load');
		htmx.trigger('#flaky-tests-container', 'load');
	}
}

// Connect on page load
document.addEventListener('DOMContentLoaded', function() {
	connectWebSocket();
});

document.body.addEventListener('htmx:beforeSwap', function(evt) {
	if (evt.detail.xhr.responseURL.includes('/api/repositories')) {
		const data = JSON.parse(evt.detail.xhr.responseText);
		evt.detail.target.innerHTML = formatRepositories(data.repositories || []);
	}
	if (evt.detail.xhr.responseURL.includes('/api/workflows')) {
		const data = JSON.parse(evt.detail.xhr.responseText);
		evt.detail.target.innerHTML = formatWorkflows(data.workflows || []);
	}
	if (evt.detail.xhr.responseURL.includes('/api/analytics')) {
		const data = JSON.parse(evt.detail.xhr.responseText);
		evt.detail.target.innerHTML = formatAnalytics(data);
	}
	if (evt.detail.xhr.responseURL.includes('/api/flaky-tests')) {
		const data = JSON.parse(evt.detail.xhr.responseText);
		evt.detail.target.innerHTML = formatFlakyTests(data.flaky_tests || []);
		updateFlakyCount(data.flaky_tests || []);
	}
});

function formatRepositories(repos) {
	if (repos.length === 0) {
		return '<div class="bg-white rounded-lg shadow p-6 text-center text-gray-500">No repositories tracked yet. Install the GitHub App to start monitoring workflows.</div>';
	}
	return '<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">' +
		repos.map(repo => {
			const successRate = repo.success_rate ? repo.success_rate.toFixed(1) : '0.0';
			const statusColor = successRate >= 90 ? 'text-green-600' : successRate >= 70 ? 'text-yellow-600' : 'text-red-600';
			return `
				<div class="bg-white rounded-lg shadow p-6 hover:shadow-lg transition-shadow">
					<h3 class="font-semibold text-lg mb-2">
						<a href="https://github.com/${repo.full_name}" target="_blank" class="text-blue-600 hover:underline">
							${repo.full_name}
						</a>
					</h3>
					<div class="space-y-2 text-sm text-gray-600">
						<div class="flex justify-between">
							<span>Total Runs:</span>
							<span class="font-medium">${repo.total_runs || 0}</span>
						</div>
						<div class="flex justify-between">
							<span>Success Rate:</span>
							<span class="font-medium ${statusColor}">${successRate}%</span>
						</div>
						<div class="flex justify-between">
							<span>Successful:</span>
							<span class="font-medium text-green-600">${repo.successful_runs || 0}</span>
						</div>
						<div class="flex justify-between">
							<span>Failed:</span>
							<span class="font-medium text-red-600">${repo.failed_runs || 0}</span>
						</div>
					</div>
					<button
						class="mt-4 w-full bg-blue-500 hover:bg-blue-600 text-white font-medium py-2 px-4 rounded"
						hx-get="/api/workflows?repository_id=${repo.id}&limit=10"
						hx-target="#workflow-detail"
						hx-swap="innerHTML">
						View Workflows
					</button>
				</div>
			`;
		}).join('') + '</div><div id="workflow-detail" class="mt-6"></div>';
}

function formatWorkflows(workflows) {
	if (workflows.length === 0) {
		return '<div class="bg-white rounded-lg shadow p-6 text-center text-gray-500">No workflow runs yet.</div>';
	}
	return '<div class="bg-white rounded-lg shadow overflow-hidden"><table class="min-w-full divide-y divide-gray-200">' +
		'<thead class="bg-gray-50"><tr>' +
		'<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Repository</th>' +
		'<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Workflow</th>' +
		'<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Branch</th>' +
		'<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Status</th>' +
		'<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Time</th>' +
		'</tr></thead><tbody class="bg-white divide-y divide-gray-200">' +
		workflows.map(wf => {
			const statusBadge = getStatusBadge(wf.conclusion || wf.status);
			const time = new Date(wf.created_at).toLocaleString();
			return `
				<tr class="hover:bg-gray-50">
					<td class="px-6 py-4 text-sm"><a href="https://github.com/${wf.repository_full_name}" target="_blank" class="text-blue-600 hover:underline">${wf.repository_full_name}</a></td>
					<td class="px-6 py-4 text-sm font-medium">${wf.workflow_name}</td>
					<td class="px-6 py-4 text-sm text-gray-600">${wf.head_branch}</td>
					<td class="px-6 py-4 text-sm">${statusBadge}</td>
					<td class="px-6 py-4 text-sm text-gray-600">${time}</td>
				</tr>
			`;
		}).join('') + '</tbody></table></div>';
}

function getStatusBadge(status) {
	const badges = {
		'success': '<span class="px-2 py-1 text-xs font-semibold rounded-full bg-green-100 text-green-800">✓ Success</span>',
		'failure': '<span class="px-2 py-1 text-xs font-semibold rounded-full bg-red-100 text-red-800">✗ Failed</span>',
		'cancelled': '<span class="px-2 py-1 text-xs font-semibold rounded-full bg-gray-100 text-gray-800">Cancelled</span>',
		'in_progress': '<span class="px-2 py-1 text-xs font-semibold rounded-full bg-blue-100 text-blue-800">⟳ Running</span>',
		'queued': '<span class="px-2 py-1 text-xs font-semibold rounded-full bg-yellow-100 text-yellow-800">Queued</span>',
	};
	return badges[status] || '<span class="px-2 py-1 text-xs font-semibold rounded-full bg-gray-100 text-gray-800">' + status + '</span>';
}

function formatAnalytics(data) {
	const successRate = data.success_rate ? data.success_rate.toFixed(1) : '0.0';
	const statusColor = successRate >= 90 ? 'text-green-600' : successRate >= 70 ? 'text-yellow-600' : 'text-red-600';
	
	return `
		<div class="bg-white rounded-lg shadow p-6">
			<h3 class="text-lg font-semibold text-gray-900 mb-2">Last 7 Days</h3>
			<div class="text-3xl font-bold ${statusColor} mb-2">${successRate}%</div>
			<p class="text-sm text-gray-600">${data.total_runs || 0} total runs</p>
		</div>
	`;
}

function formatFlakyTests(tests) {
	if (tests.length === 0) {
		return '<div class="bg-white rounded-lg shadow p-6 text-center text-gray-500">No flaky tests detected in the last 30 days.</div>';
	}
	
	return `
		<div class="bg-white rounded-lg shadow overflow-hidden">
			<table class="min-w-full divide-y divide-gray-200">
				<thead class="bg-gray-50">
					<tr>
						<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Test Name</th>
						<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Repository</th>
						<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Flake Rate</th>
						<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Total Runs</th>
						<th class="px-6 py-3 text-left text-xs font-medium text-gray-500 uppercase">Failures</th>
					</tr>
				</thead>
				<tbody class="bg-white divide-y divide-gray-200">
					${tests.map(test => {
						const flakeRate = (test.flake_rate * 100).toFixed(1);
						const rateColor = test.flake_rate > 0.5 ? 'text-red-600' : test.flake_rate > 0.2 ? 'text-yellow-600' : 'text-orange-600';
						return `
							<tr class="hover:bg-gray-50">
								<td class="px-6 py-4 text-sm font-medium">
									<a href="${test.html_url}" target="_blank" class="text-blue-600 hover:underline">
										${test.check_name}
									</a>
								</td>
								<td class="px-6 py-4 text-sm text-gray-600">${test.repository_full_name}</td>
								<td class="px-6 py-4 text-sm font-semibold ${rateColor}">${flakeRate}%</td>
								<td class="px-6 py-4 text-sm text-gray-600">${test.total_runs}</td>
								<td class="px-6 py-4 text-sm text-red-600">${test.failure_count}</td>
							</tr>
						`;
					}).join('')}
				</tbody>
			</table>
		</div>
	`;
}

function updateFlakyCount(tests) {
	const countEl = document.getElementById('flaky-count');
	if (countEl) {
		countEl.textContent = tests.length;
		countEl.className = tests.length > 0 ? 'text-3xl font-bold text-yellow-600' : 'text-3xl font-bold text-green-600';
	}
	
	const trendEl = document.getElementById('trend-indicator');
	if (trendEl && tests.length > 0) {
		const avgFlakeRate = tests.reduce((sum, t) => sum + t.flake_rate, 0) / tests.length;
		const avgSuccess = ((1 - avgFlakeRate) * 100).toFixed(1);
		trendEl.textContent = `${avgSuccess}%`;
		trendEl.className = avgSuccess >= 90 ? 'text-3xl font-bold text-green-600' : 
		                    avgSuccess >= 70 ? 'text-3xl font-bold text-yellow-600' : 
		                    'text-3xl font-bold text-red-600';
	}
}

