// Dashboard state
let latencyChart = null;
let throughputChart = null;
let allResults = [];

// Colors for protocols
const COLORS = {
    grpc: 'rgba(66, 133, 244, 0.8)',
    rest: 'rgba(234, 67, 53, 0.8)'
};

// Fetch results from API
async function fetchResults(filters = {}) {
    const params = new URLSearchParams();
    if (filters.scenario) params.set('scenario', filters.scenario);
    if (filters.protocol) params.set('protocol', filters.protocol);
    if (filters.client) params.set('client', filters.client);

    const url = `/api/v1/results?${params.toString()}`;
    const response = await fetch(url);
    if (!response.ok) {
        throw new Error(`HTTP ${response.status}: ${response.statusText}`);
    }
    return response.json();
}

// Get current filter values
function getFilters() {
    return {
        scenario: document.getElementById('scenario-filter').value,
        protocol: document.getElementById('protocol-filter').value,
        client: document.getElementById('client-filter').value
    };
}

// Get latest result per configuration
function getLatestPerConfig(results) {
    const latest = {};
    for (const r of results) {
        const key = `${r.scenario}-${r.protocol}-${r.client}-${r.concurrency}`;
        if (!latest[key] || r.run_id > latest[key].run_id) {
            latest[key] = r;
        }
    }
    return Object.values(latest);
}

// Update summary statistics
function updateSummary(results) {
    document.getElementById('total-runs').textContent = results.length;

    if (results.length > 0) {
        const bestThroughput = Math.max(...results.map(r => r.throughput));
        const validLatencies = results.filter(r => r.p99_latency_ms > 0).map(r => r.p99_latency_ms);
        const bestLatency = validLatencies.length > 0 ? Math.min(...validLatencies) : 0;

        document.getElementById('best-throughput').textContent = bestThroughput.toFixed(1);
        document.getElementById('best-latency').textContent = bestLatency > 0 ? bestLatency.toFixed(2) : '-';
    } else {
        document.getElementById('best-throughput').textContent = '-';
        document.getElementById('best-latency').textContent = '-';
    }
}

// Render latency chart
function renderLatencyChart(results) {
    const ctx = document.getElementById('latency-chart').getContext('2d');

    // Destroy existing chart
    if (latencyChart) {
        latencyChart.destroy();
    }

    // Get latest results per configuration
    const latestResults = getLatestPerConfig(results);

    // Create labels (protocol-client combinations)
    const labels = [...new Set(latestResults.map(r => `${r.protocol}-${r.client}`))];

    // Build datasets for p50, p90, p99
    const p50Data = [];
    const p90Data = [];
    const p99Data = [];

    for (const label of labels) {
        const matching = latestResults.filter(r => `${r.protocol}-${r.client}` === label);
        if (matching.length > 0) {
            // Average across concurrency levels
            p50Data.push(matching.reduce((sum, r) => sum + r.p50_latency_ms, 0) / matching.length);
            p90Data.push(matching.reduce((sum, r) => sum + r.p90_latency_ms, 0) / matching.length);
            p99Data.push(matching.reduce((sum, r) => sum + r.p99_latency_ms, 0) / matching.length);
        }
    }

    latencyChart = new Chart(ctx, {
        type: 'bar',
        data: {
            labels: labels,
            datasets: [
                {
                    label: 'p50',
                    data: p50Data,
                    backgroundColor: 'rgba(75, 192, 192, 0.8)',
                    borderColor: 'rgba(75, 192, 192, 1)',
                    borderWidth: 1
                },
                {
                    label: 'p90',
                    data: p90Data,
                    backgroundColor: 'rgba(255, 206, 86, 0.8)',
                    borderColor: 'rgba(255, 206, 86, 1)',
                    borderWidth: 1
                },
                {
                    label: 'p99',
                    data: p99Data,
                    backgroundColor: 'rgba(255, 99, 132, 0.8)',
                    borderColor: 'rgba(255, 99, 132, 1)',
                    borderWidth: 1
                }
            ]
        },
        options: {
            responsive: true,
            plugins: {
                legend: {
                    position: 'top'
                },
                title: {
                    display: false
                }
            },
            scales: {
                y: {
                    beginAtZero: true,
                    title: {
                        display: true,
                        text: 'Latency (ms)'
                    }
                }
            }
        }
    });
}

// Render throughput chart
function renderThroughputChart(results) {
    const ctx = document.getElementById('throughput-chart').getContext('2d');

    // Destroy existing chart
    if (throughputChart) {
        throughputChart.destroy();
    }

    // Get latest results per configuration
    const latestResults = getLatestPerConfig(results);

    // Create labels and data
    const labels = [...new Set(latestResults.map(r => `${r.protocol}-${r.client}`))];
    const throughputData = [];
    const backgroundColors = [];

    for (const label of labels) {
        const matching = latestResults.filter(r => `${r.protocol}-${r.client}` === label);
        if (matching.length > 0) {
            // Max throughput across concurrency levels
            throughputData.push(Math.max(...matching.map(r => r.throughput)));
            // Color based on protocol
            const protocol = label.split('-')[0];
            backgroundColors.push(protocol === 'grpc' ? COLORS.grpc : COLORS.rest);
        }
    }

    throughputChart = new Chart(ctx, {
        type: 'bar',
        data: {
            labels: labels,
            datasets: [
                {
                    label: 'Throughput',
                    data: throughputData,
                    backgroundColor: backgroundColors,
                    borderColor: backgroundColors.map(c => c.replace('0.8', '1')),
                    borderWidth: 1
                }
            ]
        },
        options: {
            responsive: true,
            plugins: {
                legend: {
                    display: false
                }
            },
            scales: {
                y: {
                    beginAtZero: true,
                    title: {
                        display: true,
                        text: 'Requests/sec'
                    }
                }
            }
        }
    });
}

// Render results table
function renderTable(results) {
    const tbody = document.getElementById('results-body');
    tbody.innerHTML = '';

    for (const r of results) {
        const successRate = r.total_samples > 0
            ? ((r.successful / r.total_samples) * 100).toFixed(1) + '%'
            : '-';

        const row = document.createElement('tr');
        row.innerHTML = `
            <td>${r.run_id}</td>
            <td>${r.scenario}</td>
            <td class="${r.protocol}">${r.protocol}</td>
            <td>${r.client}</td>
            <td>${r.concurrency}</td>
            <td>${r.throughput.toFixed(1)}</td>
            <td>${r.p50_latency_ms.toFixed(2)}</td>
            <td>${r.p90_latency_ms.toFixed(2)}</td>
            <td>${r.p99_latency_ms.toFixed(2)}</td>
            <td>${successRate}</td>
        `;
        tbody.appendChild(row);
    }
}

// Refresh dashboard with current filters
async function refreshDashboard() {
    try {
        const filters = getFilters();
        const data = await fetchResults(filters);
        allResults = data.results || [];

        updateSummary(allResults);
        renderLatencyChart(allResults);
        renderThroughputChart(allResults);
        renderTable(allResults);
    } catch (error) {
        console.error('Failed to fetch results:', error);
        document.getElementById('results-body').innerHTML =
            `<tr><td colspan="10" class="error">Failed to load results: ${error.message}</td></tr>`;
    }
}

// Initialize dashboard
document.addEventListener('DOMContentLoaded', () => {
    // Add event listeners
    document.getElementById('refresh-btn').addEventListener('click', refreshDashboard);
    document.getElementById('scenario-filter').addEventListener('change', refreshDashboard);
    document.getElementById('protocol-filter').addEventListener('change', refreshDashboard);
    document.getElementById('client-filter').addEventListener('change', refreshDashboard);

    // Initial load
    refreshDashboard();
});
