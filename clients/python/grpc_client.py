#!/usr/bin/env python3
"""
Python gRPC benchmark client for comparing against Go implementation.
Implements the same scenarios as the Go benchmark runner.
"""

import argparse
import random
import re
import signal
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass
from datetime import datetime
from threading import Event, Lock
from typing import Optional

import grpc


def parse_duration(value: str) -> int:
    """Parse a duration string (e.g., '10s', '1m', '30') into seconds."""
    value = value.strip()
    # Try plain integer first
    if value.isdigit():
        return int(value)
    # Match duration patterns like 10s, 1m, 2h
    match = re.match(r'^(\d+)(s|m|h)?$', value.lower())
    if not match:
        raise argparse.ArgumentTypeError(f"Invalid duration: {value} (use e.g., 10, 10s, 1m)")
    num = int(match.group(1))
    unit = match.group(2) or 's'
    multipliers = {'s': 1, 'm': 60, 'h': 3600}
    return num * multipliers[unit]

# Generated proto imports (run generate_proto.sh first)
from proto import benchmark_pb2, benchmark_pb2_grpc
from resources import ResourceMonitor, ResourceStats
from database import Database, DBConfig, load_account_ids


@dataclass
class Sample:
    """Single benchmark measurement."""
    latency_ms: float
    success: bool
    error: Optional[str]
    timestamp: datetime


class Results:
    """Collects and analyzes benchmark samples."""

    def __init__(self):
        self.samples: list[Sample] = []
        self.start_time: Optional[datetime] = None
        self.end_time: Optional[datetime] = None
        self.resource_stats: Optional[ResourceStats] = None
        self._lock = Lock()

    def add(self, sample: Sample):
        with self._lock:
            self.samples.append(sample)

    def set_start_time(self, t: datetime):
        self.start_time = t

    def set_end_time(self, t: datetime):
        self.end_time = t

    def set_resource_stats(self, stats: ResourceStats):
        self.resource_stats = stats

    @property
    def total_requests(self) -> int:
        return len(self.samples)

    @property
    def successful_requests(self) -> int:
        return sum(1 for s in self.samples if s.success)

    @property
    def error_rate(self) -> float:
        if not self.samples:
            return 0.0
        errors = self.total_requests - self.successful_requests
        return errors / self.total_requests * 100

    @property
    def duration_seconds(self) -> float:
        if not self.start_time or not self.end_time:
            return 0.0
        return (self.end_time - self.start_time).total_seconds()

    @property
    def throughput(self) -> float:
        if self.duration_seconds == 0:
            return 0.0
        return self.total_requests / self.duration_seconds

    def _successful_latencies(self) -> list[float]:
        return sorted(s.latency_ms for s in self.samples if s.success and s.latency_ms > 0)

    def percentile(self, p: float) -> float:
        latencies = self._successful_latencies()
        if not latencies:
            return 0.0
        idx = int(len(latencies) * p / 100)
        idx = min(idx, len(latencies) - 1)
        return latencies[idx]

    def avg_latency(self) -> float:
        latencies = self._successful_latencies()
        if not latencies:
            return 0.0
        return sum(latencies) / len(latencies)

    def min_latency(self) -> float:
        latencies = self._successful_latencies()
        return min(latencies) if latencies else 0.0

    def max_latency(self) -> float:
        latencies = self._successful_latencies()
        return max(latencies) if latencies else 0.0

    def print_summary(self, scenario: str, protocol: str, concurrency: int):
        print(f"\nBenchmark: {scenario} / {protocol}")
        print(f"Duration: {self.duration_seconds:.0f}s | Concurrency: {concurrency}")
        print("-" * 33)
        print(f"Requests:    {self.total_requests}")
        print(f"Throughput:  {self.throughput:.2f} req/s")
        print("Latency:")
        print(f"  p50:  {self.percentile(50):.2f}ms")
        print(f"  p90:  {self.percentile(90):.2f}ms")
        print(f"  p99:  {self.percentile(99):.2f}ms")
        print(f"  avg:  {self.avg_latency():.2f}ms")
        print(f"  min:  {self.min_latency():.2f}ms")
        print(f"  max:  {self.max_latency():.2f}ms")
        print(f"Errors:      {self.total_requests - self.successful_requests} ({self.error_rate:.2f}%)")
        if self.resource_stats:
            print("Resources:")
            print(f"  CPU avg:   {self.resource_stats.cpu_avg_percent:.1f}%")
            print(f"  Mem avg:   {self.resource_stats.memory_avg_mb:.1f} MB")
            print(f"  Mem peak:  {self.resource_stats.memory_peak_mb:.1f} MB")
        print()

    def store_results(
        self,
        db: Database,
        scenario: str,
        protocol: str,
        client: str,
        concurrency: int,
        rate_limit: Optional[int],
    ):
        """Save benchmark results to PostgreSQL."""
        with db.get_connection() as conn:
            with conn.cursor() as cur:
                # Insert benchmark run with resource stats
                cpu_avg = self.resource_stats.cpu_avg_percent if self.resource_stats else None
                mem_avg = self.resource_stats.memory_avg_mb if self.resource_stats else None
                mem_peak = self.resource_stats.memory_peak_mb if self.resource_stats else None

                cur.execute(
                    """
                    INSERT INTO benchmark_runs (scenario, protocol, client, concurrency, duration_sec, rate_limit,
                                                cpu_usage_avg, memory_mb_avg, memory_mb_peak)
                    VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s)
                    RETURNING id
                    """,
                    (scenario, protocol, client, concurrency, int(self.duration_seconds), rate_limit,
                     cpu_avg, mem_avg, mem_peak),
                )
                run_id = cur.fetchone()[0]

                # Batch insert samples using COPY
                with cur.copy(
                    "COPY benchmark_samples (run_id, latency_ms, success, error_type, timestamp) FROM STDIN"
                ) as copy:
                    for sample in self.samples:
                        copy.write_row((
                            run_id,
                            sample.latency_ms,
                            sample.success,
                            sample.error,
                            sample.timestamp,
                        ))

                conn.commit()
                print(f"Results saved to database (run_id: {run_id})")

                # Retrieve stats from view
                cur.execute(
                    """
                    SELECT p50_latency, p90_latency, p99_latency
                    FROM benchmark_stats
                    WHERE run_id = %s
                    """,
                    (run_id,),
                )
                row = cur.fetchone()
                if row:
                    print(f"\nDatabase stats (from benchmark_stats view):")
                    print(f"  p50: {row[0]:.2f}ms, p90: {row[1]:.2f}ms, p99: {row[2]:.2f}ms")


class GRPCBenchmarkClient:
    """gRPC client for benchmark scenarios."""

    def __init__(self, addr: str):
        self.channel = grpc.insecure_channel(addr)
        self.balance_stub = benchmark_pb2_grpc.BalanceServiceStub(self.channel)
        self.tx_stub = benchmark_pb2_grpc.TransactionServiceStub(self.channel)

    def get_balance(self, account_id: str) -> None:
        """Execute a single balance query."""
        self.balance_stub.GetBalance(benchmark_pb2.BalanceRequest(account_id=account_id))

    def stream_transactions(self, rate: int, stop_event: Event):
        """Subscribe to transaction stream, yielding events until stopped."""
        request = benchmark_pb2.StreamRequest(rate_limit=rate)
        try:
            for tx in self.tx_stub.StreamTransactions(request):
                if stop_event.is_set():
                    break
                yield tx
        except grpc.RpcError:
            if not stop_event.is_set():
                raise

    def close(self):
        self.channel.close()


class Runner:
    """Manages benchmark load generation."""

    def __init__(
        self,
        client: GRPCBenchmarkClient,
        account_ids: list[str],
        concurrency: int,
        rate: int,
    ):
        self.client = client
        self.account_ids = account_ids
        self.concurrency = concurrency
        self.rate = rate
        self.results = Results()
        self._stop_event = Event()
        self._rng_lock = Lock()
        self._rng = random.Random()

    def stop(self):
        self._stop_event.set()

    def _random_account(self) -> str:
        with self._rng_lock:
            return self._rng.choice(self.account_ids)

    def run_balance(self, duration_sec: float):
        """Execute balance query benchmark with concurrent workers."""
        self.results.set_start_time(datetime.now())
        end_time = time.time() + duration_sec

        def worker():
            while time.time() < end_time and not self._stop_event.is_set():
                account_id = self._random_account()
                start = time.time()
                error = None
                success = True

                try:
                    self.client.get_balance(account_id)
                except Exception as e:
                    success = False
                    error = str(e)

                latency_ms = (time.time() - start) * 1000
                self.results.add(Sample(
                    latency_ms=latency_ms,
                    success=success,
                    error=error,
                    timestamp=datetime.now(),
                ))

        with ThreadPoolExecutor(max_workers=self.concurrency) as executor:
            futures = [executor.submit(worker) for _ in range(self.concurrency)]
            for future in as_completed(futures):
                future.result()  # Raise any exceptions

        self.results.set_end_time(datetime.now())

    def run_stream(self, duration_sec: float):
        """Execute streaming benchmark with concurrent stream consumers."""
        self.results.set_start_time(datetime.now())
        end_time = time.time() + duration_sec

        def worker():
            last_event_time = None
            try:
                for _ in self.client.stream_transactions(self.rate, self._stop_event):
                    if time.time() >= end_time:
                        break

                    now = time.time()
                    latency_ms = 0.0
                    if last_event_time is not None:
                        latency_ms = (now - last_event_time) * 1000
                    last_event_time = now

                    self.results.add(Sample(
                        latency_ms=latency_ms,
                        success=True,
                        error=None,
                        timestamp=datetime.now(),
                    ))
            except Exception as e:
                if not self._stop_event.is_set():
                    self.results.add(Sample(
                        latency_ms=0,
                        success=False,
                        error=str(e),
                        timestamp=datetime.now(),
                    ))

        with ThreadPoolExecutor(max_workers=self.concurrency) as executor:
            futures = [executor.submit(worker) for _ in range(self.concurrency)]

            # Wait for duration or stop
            while time.time() < end_time and not self._stop_event.is_set():
                time.sleep(0.1)

            self._stop_event.set()
            for future in as_completed(futures):
                try:
                    future.result()
                except Exception:
                    pass

        self.results.set_end_time(datetime.now())


def main():
    parser = argparse.ArgumentParser(description="Python gRPC benchmark client")
    parser.add_argument("--scenario", default="balance", choices=["balance", "stream"],
                        help="Benchmark scenario")
    parser.add_argument("--concurrency", type=int, default=10,
                        help="Number of parallel workers")
    parser.add_argument("--duration", type=parse_duration, default=30,
                        help="Test duration (e.g., 30, 30s, 1m)")
    parser.add_argument("--rate", type=int, default=0,
                        help="Events per second for streaming (0 = unlimited)")
    parser.add_argument("--grpc-addr", default="localhost:50051",
                        help="gRPC server address")

    # Database flags
    parser.add_argument("--db-host", default="localhost")
    parser.add_argument("--db-port", type=int, default=5432)
    parser.add_argument("--db-user", default="benchmark")
    parser.add_argument("--db-pass", default="benchmark_pass")
    parser.add_argument("--db-name", default="grpc_benchmark")

    args = parser.parse_args()

    # Connect to database with pooling
    db_config = DBConfig(
        host=args.db_host,
        port=args.db_port,
        user=args.db_user,
        password=args.db_pass,
        database=args.db_name,
    )
    db = Database(db_config)
    db.connect()
    print(f"Connected to database {args.db_name}@{args.db_host}:{args.db_port} (pooled)")

    # Load account IDs for balance scenario
    account_ids = []
    if args.scenario == "balance":
        print("Loading account IDs from database...")
        account_ids = load_account_ids(db)
        if not account_ids:
            print("No accounts found in database. Run 'make seed' first.")
            sys.exit(1)
        print(f"Loaded {len(account_ids)} account IDs")

    # Create gRPC client
    client = GRPCBenchmarkClient(args.grpc_addr)
    print(f"Connected to gRPC server at {args.grpc_addr}")

    # Create runner
    runner = Runner(client, account_ids, args.concurrency, args.rate)

    # Handle interrupt signals
    def signal_handler(sig, frame):
        print("\nReceived interrupt signal, stopping benchmark...")
        runner.stop()

    signal.signal(signal.SIGINT, signal_handler)
    signal.signal(signal.SIGTERM, signal_handler)

    # Run benchmark
    print(f"\nStarting {args.scenario} benchmark (grpc protocol, python client)")
    print(f"Concurrency: {args.concurrency} | Duration: {args.duration}s", end="")
    if args.scenario == "stream" and args.rate > 0:
        print(f" | Rate limit: {args.rate} events/s", end="")
    print("\n")

    # Start resource monitoring
    resource_monitor = ResourceMonitor(interval_ms=100)
    stop_monitor = resource_monitor.start()

    if args.scenario == "balance":
        runner.run_balance(args.duration)
    else:
        runner.run_stream(args.duration)

    # Stop resource monitoring and record stats
    resource_stats = stop_monitor()
    runner.results.set_resource_stats(resource_stats)

    # Print and store results
    runner.results.print_summary(args.scenario, "grpc", args.concurrency)

    rate_limit = args.rate if args.scenario == "stream" and args.rate > 0 else None
    runner.results.store_results(
        db,
        scenario=args.scenario,
        protocol="grpc",
        client="python-grpc",
        concurrency=args.concurrency,
        rate_limit=rate_limit,
    )

    client.close()
    db.close()


if __name__ == "__main__":
    main()
