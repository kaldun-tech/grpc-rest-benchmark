-- Add resource usage columns to benchmark_runs
ALTER TABLE benchmark_runs ADD COLUMN cpu_usage_avg FLOAT;
ALTER TABLE benchmark_runs ADD COLUMN memory_mb_avg FLOAT;
ALTER TABLE benchmark_runs ADD COLUMN memory_mb_peak FLOAT;

-- Update the stats view to include resource metrics
DROP VIEW IF EXISTS benchmark_stats;

CREATE VIEW benchmark_stats AS
SELECT
    r.id as run_id,
    r.scenario,
    r.protocol,
    r.client,
    r.concurrency,
    r.cpu_usage_avg,
    r.memory_mb_avg,
    r.memory_mb_peak,
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
GROUP BY r.id, r.scenario, r.protocol, r.client, r.concurrency,
         r.cpu_usage_avg, r.memory_mb_avg, r.memory_mb_peak;
