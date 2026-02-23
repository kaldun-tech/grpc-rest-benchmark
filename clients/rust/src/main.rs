use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant};

use clap::Parser;
use deadpool_postgres::{Config as PoolConfig, Pool, Runtime};
use futures::stream::StreamExt;
use rand::seq::SliceRandom;
use rand::SeedableRng;
use rand::rngs::StdRng;
use serde::Deserialize;
use sysinfo::System;
use tokio::sync::mpsc;
use tokio::time::interval;
use tokio_postgres::NoTls;
use tonic::transport::Channel;

pub mod benchmark {
    tonic::include_proto!("benchmark");
}

use benchmark::balance_service_client::BalanceServiceClient;
use benchmark::transaction_service_client::TransactionServiceClient;
use benchmark::{BalanceRequest, StreamRequest};

#[derive(Parser, Debug)]
#[command(name = "benchmark_client")]
#[command(about = "Rust gRPC/REST benchmark client")]
struct Args {
    /// Benchmark scenario: balance | stream
    #[arg(long, default_value = "balance")]
    scenario: String,

    /// Protocol to test: grpc | rest
    #[arg(long, default_value = "grpc")]
    protocol: String,

    /// Number of parallel workers
    #[arg(long, default_value_t = 10)]
    concurrency: usize,

    /// Test duration (e.g., 30s, 1m)
    #[arg(long, default_value = "30s")]
    duration: String,

    /// Events per second for streaming (0 = unlimited)
    #[arg(long, default_value_t = 0)]
    rate: i32,

    /// gRPC server address
    #[arg(long, default_value = "http://localhost:50051")]
    grpc_addr: String,

    /// REST server address
    #[arg(long, default_value = "http://localhost:8080")]
    rest_addr: String,

    /// PostgreSQL host
    #[arg(long, default_value = "localhost")]
    db_host: String,

    /// PostgreSQL port
    #[arg(long, default_value_t = 5432)]
    db_port: u16,

    /// PostgreSQL user
    #[arg(long, default_value = "benchmark")]
    db_user: String,

    /// PostgreSQL password
    #[arg(long, default_value = "benchmark_pass")]
    db_pass: String,

    /// PostgreSQL database
    #[arg(long, default_value = "grpc_benchmark")]
    db_name: String,
}

#[derive(Debug)]
struct Sample {
    latency: Duration,
    success: bool,
}

#[derive(Debug, Default)]
struct Results {
    samples: Vec<Sample>,
    start_time: Option<Instant>,
    end_time: Option<Instant>,
    cpu_samples: Vec<f32>,
    mem_samples: Vec<u64>,
}

#[derive(Debug, Deserialize)]
struct BalanceResponse {
    #[allow(dead_code)]
    account_id: String,
    #[allow(dead_code)]
    balance_tinybar: i64,
}

impl Results {
    fn add_sample(&mut self, sample: Sample) {
        self.samples.push(sample);
    }

    fn success_count(&self) -> usize {
        self.samples.iter().filter(|s| s.success).count()
    }

    fn error_count(&self) -> usize {
        self.samples.iter().filter(|s| !s.success).count()
    }

    fn latencies(&self) -> Vec<Duration> {
        self.samples
            .iter()
            .filter(|s| s.success)
            .map(|s| s.latency)
            .collect()
    }

    fn percentile(&self, p: f64) -> Duration {
        let mut latencies = self.latencies();
        if latencies.is_empty() {
            return Duration::ZERO;
        }
        latencies.sort();
        let idx = ((p / 100.0) * latencies.len() as f64) as usize;
        latencies[idx.min(latencies.len() - 1)]
    }

    fn avg_latency(&self) -> Duration {
        let latencies = self.latencies();
        if latencies.is_empty() {
            return Duration::ZERO;
        }
        let total: Duration = latencies.iter().sum();
        total / latencies.len() as u32
    }

    fn min_latency(&self) -> Duration {
        self.latencies().into_iter().min().unwrap_or(Duration::ZERO)
    }

    fn max_latency(&self) -> Duration {
        self.latencies().into_iter().max().unwrap_or(Duration::ZERO)
    }

    fn throughput(&self) -> f64 {
        match (self.start_time, self.end_time) {
            (Some(start), Some(end)) => {
                let duration = end.duration_since(start).as_secs_f64();
                if duration > 0.0 {
                    self.success_count() as f64 / duration
                } else {
                    0.0
                }
            }
            _ => 0.0,
        }
    }

    fn avg_cpu(&self) -> f32 {
        if self.cpu_samples.is_empty() {
            return 0.0;
        }
        self.cpu_samples.iter().sum::<f32>() / self.cpu_samples.len() as f32
    }

    fn avg_mem_mb(&self) -> f64 {
        if self.mem_samples.is_empty() {
            return 0.0;
        }
        let avg_bytes = self.mem_samples.iter().sum::<u64>() as f64 / self.mem_samples.len() as f64;
        avg_bytes / 1024.0 / 1024.0
    }

    fn peak_mem_mb(&self) -> f64 {
        self.mem_samples.iter().max().copied().unwrap_or(0) as f64 / 1024.0 / 1024.0
    }

    fn print_summary(&self, scenario: &str, protocol: &str, concurrency: usize) {
        let duration = match (self.start_time, self.end_time) {
            (Some(start), Some(end)) => end.duration_since(start),
            _ => Duration::ZERO,
        };

        println!();
        println!("Benchmark: {} / {}", scenario, protocol);
        println!("Duration: {:?} | Concurrency: {}", duration, concurrency);
        println!("---------------------------------");
        println!("Requests:    {}", self.samples.len());
        println!("Throughput:  {:.2} req/s", self.throughput());
        println!("Latency:");
        println!("  p50:  {:?}", self.percentile(50.0));
        println!("  p90:  {:?}", self.percentile(90.0));
        println!("  p99:  {:?}", self.percentile(99.0));
        println!("  avg:  {:?}", self.avg_latency());
        println!("  min:  {:?}", self.min_latency());
        println!("  max:  {:?}", self.max_latency());
        println!(
            "Errors:      {} ({:.2}%)",
            self.error_count(),
            self.error_count() as f64 / self.samples.len().max(1) as f64 * 100.0
        );
        println!("Resources:");
        println!("  CPU avg:   {:.1}%", self.avg_cpu());
        println!("  Mem avg:   {:.1} MB", self.avg_mem_mb());
        println!("  Mem peak:  {:.1} MB", self.peak_mem_mb());
    }
}

fn parse_duration(s: &str) -> Result<Duration, String> {
    let s = s.trim();
    if s.ends_with("ms") {
        let val: u64 = s.trim_end_matches("ms").parse().map_err(|e| format!("{}", e))?;
        Ok(Duration::from_millis(val))
    } else if s.ends_with('s') {
        let val: u64 = s.trim_end_matches('s').parse().map_err(|e| format!("{}", e))?;
        Ok(Duration::from_secs(val))
    } else if s.ends_with('m') {
        let val: u64 = s.trim_end_matches('m').parse().map_err(|e| format!("{}", e))?;
        Ok(Duration::from_secs(val * 60))
    } else {
        Err(format!("Invalid duration format: {}", s))
    }
}

/// Create a database connection pool with retry logic.
fn create_db_pool(
    db_host: &str,
    db_port: u16,
    db_user: &str,
    db_pass: &str,
    db_name: &str,
) -> Result<Pool, Box<dyn std::error::Error>> {
    let mut cfg = PoolConfig::new();
    cfg.host = Some(db_host.to_string());
    cfg.port = Some(db_port);
    cfg.user = Some(db_user.to_string());
    cfg.password = Some(db_pass.to_string());
    cfg.dbname = Some(db_name.to_string());

    // Pool configuration
    cfg.pool = Some(deadpool_postgres::PoolConfig {
        max_size: 50,
        timeouts: deadpool_postgres::Timeouts::default(),
        ..Default::default()
    });

    let pool = cfg.create_pool(Some(Runtime::Tokio1), NoTls)?;
    Ok(pool)
}

/// Connect to database with retry logic.
async fn connect_with_retry(pool: &Pool, max_retries: u32) -> Result<deadpool_postgres::Object, Box<dyn std::error::Error>> {
    let mut last_err = None;
    let mut retry_interval = Duration::from_millis(100);

    for attempt in 0..=max_retries {
        if attempt > 0 {
            tokio::time::sleep(retry_interval).await;
            retry_interval *= 2; // Exponential backoff
        }

        match pool.get().await {
            Ok(client) => return Ok(client),
            Err(e) => {
                last_err = Some(e);
            }
        }
    }

    Err(format!("Failed to connect after {} retries: {:?}", max_retries, last_err).into())
}

async fn fetch_account_ids(pool: &Pool) -> Result<Vec<String>, Box<dyn std::error::Error>> {
    let client = connect_with_retry(pool, 3).await?;

    let rows = client
        .query("SELECT account_id FROM accounts", &[])
        .await?;

    let account_ids: Vec<String> = rows.iter().map(|row| row.get(0)).collect();
    Ok(account_ids)
}

async fn store_results(
    pool: &Pool,
    scenario: &str,
    protocol: &str,
    concurrency: usize,
    results: &Results,
) -> Result<i32, Box<dyn std::error::Error>> {
    let client = connect_with_retry(pool, 3).await?;

    let duration = match (results.start_time, results.end_time) {
        (Some(start), Some(end)) => end.duration_since(start),
        _ => Duration::ZERO,
    };

    // Insert run record
    let client_name = format!("rust-{}", protocol);
    let row = client
        .query_one(
            "INSERT INTO benchmark_runs (scenario, protocol, client, concurrency, duration_sec, \
             cpu_usage_avg, memory_mb_avg, memory_mb_peak) \
             VALUES ($1, $2, $3, $4, $5, $6, $7, $8) \
             RETURNING id",
            &[
                &scenario,
                &protocol,
                &client_name.as_str(),
                &(concurrency as i32),
                &(duration.as_secs() as i32),
                &(results.avg_cpu() as f64),
                &results.avg_mem_mb(),
                &results.peak_mem_mb(),
            ],
        )
        .await?;

    let run_id: i32 = row.get(0);

    // Insert samples (batch insert for performance)
    let now = chrono::Utc::now().naive_utc();
    for chunk in results.samples.chunks(1000) {
        let mut query = String::from(
            "INSERT INTO benchmark_samples (run_id, latency_ms, success, timestamp) VALUES "
        );
        let mut values: Vec<String> = Vec::new();
        for (i, _sample) in chunk.iter().enumerate() {
            let idx = i * 4;
            values.push(format!(
                "(${}, ${}, ${}, ${})",
                idx + 1, idx + 2, idx + 3, idx + 4
            ));
        }
        query.push_str(&values.join(", "));

        // Build params
        let mut params: Vec<Box<dyn tokio_postgres::types::ToSql + Sync>> = Vec::new();
        for sample in chunk {
            let latency_ms = sample.latency.as_secs_f64() * 1000.0;
            params.push(Box::new(run_id));
            params.push(Box::new(latency_ms));
            params.push(Box::new(sample.success));
            params.push(Box::new(now));
        }
        let params_refs: Vec<&(dyn tokio_postgres::types::ToSql + Sync)> =
            params.iter().map(|p| p.as_ref()).collect();
        client.execute(&query, &params_refs).await?;
    }

    println!("\nResults saved to database (run_id: {})", run_id);

    Ok(run_id)
}

async fn run_grpc_balance(
    addr: &str,
    account_ids: Vec<String>,
    concurrency: usize,
    duration: Duration,
) -> Results {
    let mut results = Results::default();
    let (tx, mut rx) = mpsc::channel::<Sample>(10000);
    let running = Arc::new(AtomicBool::new(true));
    let request_count = Arc::new(AtomicU64::new(0));

    // Start resource monitoring
    let cpu_samples = Arc::new(tokio::sync::Mutex::new(Vec::new()));
    let mem_samples = Arc::new(tokio::sync::Mutex::new(Vec::new()));
    let monitor_running = running.clone();
    let cpu_samples_clone = cpu_samples.clone();
    let mem_samples_clone = mem_samples.clone();

    tokio::spawn(async move {
        let mut sys = System::new_all();
        let pid = sysinfo::get_current_pid().unwrap();
        let mut interval = interval(Duration::from_millis(100));

        while monitor_running.load(Ordering::Relaxed) {
            interval.tick().await;
            sys.refresh_all();

            if let Some(process) = sys.process(pid) {
                cpu_samples_clone.lock().await.push(process.cpu_usage());
                mem_samples_clone.lock().await.push(process.memory());
            }
        }
    });

    results.start_time = Some(Instant::now());

    // Spawn workers
    for _ in 0..concurrency {
        let tx = tx.clone();
        let addr = addr.to_string();
        let account_ids = account_ids.clone();
        let running = running.clone();
        let request_count = request_count.clone();

        tokio::spawn(async move {
            let channel = match Channel::from_shared(addr.clone()) {
                Ok(c) => match c.connect().await {
                    Ok(ch) => ch,
                    Err(e) => {
                        eprintln!("Failed to connect: {}", e);
                        return;
                    }
                },
                Err(e) => {
                    eprintln!("Invalid URI: {}", e);
                    return;
                }
            };

            let mut client = BalanceServiceClient::new(channel);
            let mut rng = StdRng::from_entropy();

            while running.load(Ordering::Relaxed) {
                let account_id = account_ids.choose(&mut rng).unwrap().clone();
                let start = Instant::now();

                let result = client
                    .get_balance(BalanceRequest { account_id })
                    .await;

                let latency = start.elapsed();
                let success = result.is_ok();

                request_count.fetch_add(1, Ordering::Relaxed);
                let _ = tx.send(Sample { latency, success }).await;
            }
        });
    }

    drop(tx); // Drop original sender so channel closes when workers stop

    // Run for duration
    tokio::time::sleep(duration).await;
    running.store(false, Ordering::Relaxed);

    // Collect results
    while let Some(sample) = rx.recv().await {
        results.add_sample(sample);
    }

    results.end_time = Some(Instant::now());
    results.cpu_samples = cpu_samples.lock().await.clone();
    results.mem_samples = mem_samples.lock().await.clone();

    results
}

async fn run_rest_balance(
    base_url: &str,
    account_ids: Vec<String>,
    concurrency: usize,
    duration: Duration,
) -> Results {
    let mut results = Results::default();
    let (tx, mut rx) = mpsc::channel::<Sample>(10000);
    let running = Arc::new(AtomicBool::new(true));

    // Start resource monitoring
    let cpu_samples = Arc::new(tokio::sync::Mutex::new(Vec::new()));
    let mem_samples = Arc::new(tokio::sync::Mutex::new(Vec::new()));
    let monitor_running = running.clone();
    let cpu_samples_clone = cpu_samples.clone();
    let mem_samples_clone = mem_samples.clone();

    tokio::spawn(async move {
        let mut sys = System::new_all();
        let pid = sysinfo::get_current_pid().unwrap();
        let mut interval = interval(Duration::from_millis(100));

        while monitor_running.load(Ordering::Relaxed) {
            interval.tick().await;
            sys.refresh_all();

            if let Some(process) = sys.process(pid) {
                cpu_samples_clone.lock().await.push(process.cpu_usage());
                mem_samples_clone.lock().await.push(process.memory());
            }
        }
    });

    results.start_time = Some(Instant::now());

    // Spawn workers
    for _ in 0..concurrency {
        let tx = tx.clone();
        let base_url = base_url.to_string();
        let account_ids = account_ids.clone();
        let running = running.clone();

        tokio::spawn(async move {
            let client = reqwest::Client::builder()
                .pool_max_idle_per_host(100)
                .build()
                .unwrap();
            let mut rng = StdRng::from_entropy();

            while running.load(Ordering::Relaxed) {
                let account_id = account_ids.choose(&mut rng).unwrap();
                let url = format!("{}/api/v1/accounts/{}/balance", base_url, account_id);
                let start = Instant::now();

                let result = client.get(&url).send().await;

                let latency = start.elapsed();
                let success = match result {
                    Ok(resp) => resp.status().is_success(),
                    Err(_) => false,
                };

                let _ = tx.send(Sample { latency, success }).await;
            }
        });
    }

    drop(tx);

    // Run for duration
    tokio::time::sleep(duration).await;
    running.store(false, Ordering::Relaxed);

    // Collect results
    while let Some(sample) = rx.recv().await {
        results.add_sample(sample);
    }

    results.end_time = Some(Instant::now());
    results.cpu_samples = cpu_samples.lock().await.clone();
    results.mem_samples = mem_samples.lock().await.clone();

    results
}

async fn run_grpc_stream(
    addr: &str,
    concurrency: usize,
    duration: Duration,
    rate: i32,
) -> Results {
    let mut results = Results::default();
    let (tx, mut rx) = mpsc::channel::<Sample>(10000);
    let running = Arc::new(AtomicBool::new(true));

    // Start resource monitoring
    let cpu_samples = Arc::new(tokio::sync::Mutex::new(Vec::new()));
    let mem_samples = Arc::new(tokio::sync::Mutex::new(Vec::new()));
    let monitor_running = running.clone();
    let cpu_samples_clone = cpu_samples.clone();
    let mem_samples_clone = mem_samples.clone();

    tokio::spawn(async move {
        let mut sys = System::new_all();
        let pid = sysinfo::get_current_pid().unwrap();
        let mut interval = interval(Duration::from_millis(100));

        while monitor_running.load(Ordering::Relaxed) {
            interval.tick().await;
            sys.refresh_all();

            if let Some(process) = sys.process(pid) {
                cpu_samples_clone.lock().await.push(process.cpu_usage());
                mem_samples_clone.lock().await.push(process.memory());
            }
        }
    });

    results.start_time = Some(Instant::now());

    // Spawn stream workers
    for _ in 0..concurrency {
        let tx = tx.clone();
        let addr = addr.to_string();
        let running = running.clone();

        tokio::spawn(async move {
            let channel = match Channel::from_shared(addr.clone()) {
                Ok(c) => match c.connect().await {
                    Ok(ch) => ch,
                    Err(e) => {
                        eprintln!("Failed to connect: {}", e);
                        return;
                    }
                },
                Err(e) => {
                    eprintln!("Invalid URI: {}", e);
                    return;
                }
            };

            let mut client = TransactionServiceClient::new(channel);
            let request = StreamRequest {
                since_timestamp: String::new(),
                rate_limit: rate,
                filter_account: String::new(),
            };

            let mut stream = match client.stream_transactions(request).await {
                Ok(response) => response.into_inner(),
                Err(e) => {
                    eprintln!("Failed to start stream: {}", e);
                    return;
                }
            };

            let mut last_event = Instant::now();
            while running.load(Ordering::Relaxed) {
                match stream.next().await {
                    Some(Ok(_)) => {
                        let now = Instant::now();
                        let latency = now.duration_since(last_event);
                        last_event = now;

                        let _ = tx.send(Sample { latency, success: true }).await;
                    }
                    Some(Err(e)) => {
                        eprintln!("Stream error: {}", e);
                        break;
                    }
                    None => break,
                }
            }
        });
    }

    drop(tx);

    // Run for duration
    tokio::time::sleep(duration).await;
    running.store(false, Ordering::Relaxed);

    // Collect results
    while let Some(sample) = rx.recv().await {
        results.add_sample(sample);
    }

    results.end_time = Some(Instant::now());
    results.cpu_samples = cpu_samples.lock().await.clone();
    results.mem_samples = mem_samples.lock().await.clone();

    results
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args = Args::parse();

    // Validate arguments
    if args.scenario != "balance" && args.scenario != "stream" {
        eprintln!("Invalid scenario: {} (must be 'balance' or 'stream')", args.scenario);
        std::process::exit(1);
    }
    if args.protocol != "grpc" && args.protocol != "rest" {
        eprintln!("Invalid protocol: {} (must be 'grpc' or 'rest')", args.protocol);
        std::process::exit(1);
    }

    let duration = parse_duration(&args.duration)?;

    // Create database connection pool
    println!("Connecting to database {}@{}:{}...", args.db_name, args.db_host, args.db_port);
    let pool = create_db_pool(
        &args.db_host,
        args.db_port,
        &args.db_user,
        &args.db_pass,
        &args.db_name,
    )?;
    println!("Database pool created (max_size: 50)");

    // Fetch account IDs for balance scenario
    let account_ids = if args.scenario == "balance" {
        println!("Loading account IDs from database...");
        let ids = fetch_account_ids(&pool).await?;
        println!("Loaded {} account IDs", ids.len());
        ids
    } else {
        Vec::new()
    };

    println!(
        "\nStarting {} benchmark ({} protocol)",
        args.scenario, args.protocol
    );
    println!("Concurrency: {} | Duration: {:?}", args.concurrency, duration);

    // Run benchmark
    let results = match (args.scenario.as_str(), args.protocol.as_str()) {
        ("balance", "grpc") => {
            println!("Connected to gRPC server at {}", args.grpc_addr);
            run_grpc_balance(&args.grpc_addr, account_ids, args.concurrency, duration).await
        }
        ("balance", "rest") => {
            println!("Connected to REST server at {}", args.rest_addr);
            run_rest_balance(&args.rest_addr, account_ids, args.concurrency, duration).await
        }
        ("stream", "grpc") => {
            println!("Connected to gRPC server at {}", args.grpc_addr);
            run_grpc_stream(&args.grpc_addr, args.concurrency, duration, args.rate).await
        }
        ("stream", "rest") => {
            eprintln!("REST streaming not yet implemented in Rust client");
            std::process::exit(1);
        }
        _ => unreachable!(),
    };

    // Print summary
    results.print_summary(&args.scenario, &args.protocol, args.concurrency);

    // Store results in database
    let _ = store_results(
        &pool,
        &args.scenario,
        &args.protocol,
        args.concurrency,
        &results,
    )
    .await;

    Ok(())
}
