-- Accounts table for Scenario 1: Balance queries
CREATE TABLE accounts (
    account_id TEXT PRIMARY KEY,
    balance_tinybar BIGINT NOT NULL,
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_accounts_balance ON accounts(balance_tinybar);

-- Transactions table for Scenario 2: Event streaming
CREATE TABLE transactions (
    tx_id TEXT PRIMARY KEY,
    from_account TEXT NOT NULL,
    to_account TEXT NOT NULL,
    amount_tinybar BIGINT NOT NULL,
    tx_type TEXT NOT NULL,  -- 'transfer', 'vesting_release', 'contract_call'
    timestamp TIMESTAMP NOT NULL
);

CREATE INDEX idx_transactions_timestamp ON transactions(timestamp);
CREATE INDEX idx_transactions_accounts ON transactions(from_account, to_account);

-- Benchmark results tables
CREATE TABLE benchmark_runs (
    id SERIAL PRIMARY KEY,
    scenario TEXT NOT NULL,      -- 'balance_query', 'tx_stream'
    protocol TEXT NOT NULL,      -- 'rest', 'grpc'
    concurrency INT NOT NULL,
    duration_sec INT NOT NULL,
    rate_limit INT,              -- for streaming scenarios (events/sec)
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE benchmark_samples (
    id SERIAL PRIMARY KEY,
    run_id INT NOT NULL REFERENCES benchmark_runs(id) ON DELETE CASCADE,
    latency_ms FLOAT NOT NULL,
    success BOOLEAN NOT NULL,
    error_type TEXT,             -- null if success, error category if failed
    timestamp TIMESTAMP NOT NULL
);

CREATE INDEX idx_samples_run ON benchmark_samples(run_id);
CREATE INDEX idx_samples_timestamp ON benchmark_samples(timestamp);

-- View for quick stats per run
CREATE VIEW benchmark_stats AS
SELECT
    r.id as run_id,
    r.scenario,
    r.protocol,
    r.concurrency,
    COUNT(s.id) as total_samples,
    SUM(CASE WHEN s.success THEN 1 ELSE 0 END) as successful,
    PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY s.latency_ms) as p50_latency,
    PERCENTILE_CONT(0.9) WITHIN GROUP (ORDER BY s.latency_ms) as p90_latency,
    PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY s.latency_ms) as p99_latency,
    AVG(s.latency_ms) as avg_latency,
    MIN(s.latency_ms) as min_latency,
    MAX(s.latency_ms) as max_latency
FROM benchmark_runs r
LEFT JOIN benchmark_samples s ON s.run_id = r.id
GROUP BY r.id, r.scenario, r.protocol, r.concurrency;
