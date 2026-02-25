#!/usr/bin/env python3
"""
Python Hedera SDK benchmark client for measuring SDK abstraction overhead.

This client uses the Hedera Python SDK to query account balances on the Hedera
network (testnet by default). Unlike the raw gRPC client that talks to our local
servers, this measures real SDK overhead against Hedera's infrastructure.

Comparison methodology:
- python-grpc: Raw gRPC to local server (transport + local DB)
- python-sdk: Hedera SDK to testnet (SDK overhead + network + Hedera consensus)

The SDK adds abstraction layers (retries, signing, query construction) that we
want to quantify. Network latency will dominate, but p50/p99 spread shows SDK overhead.
"""

import argparse
import os
import random
import re
import signal
import sys
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from threading import Event, Lock
from typing import Optional

from dotenv import load_dotenv


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

from database import Database, DBConfig
from resources import ResourceMonitor, ResourceStats

try:
    from hiero_sdk_python import (
        Client,
        Network,
        AccountId,
        PrivateKey,
        CryptoGetAccountBalanceQuery,
    )
except ImportError:
    print("Hedera SDK not installed. Run: pip install hiero-sdk-python")
    sys.exit(1)


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


class HederaSDKClient:
    """Hedera SDK client for benchmark scenarios."""

    def __init__(self, network: str, operator_id: str, operator_key: str):
        """
        Initialize Hedera SDK client.

        Args:
            network: Network name ('testnet', 'mainnet', 'previewnet')
            operator_id: Operator account ID (e.g., '0.0.12345')
            operator_key: Operator private key (DER encoded hex string)
        """
        self.network_name = network
        self._network = Network(network)
        self._client = Client(self._network)

        self._operator_id = AccountId.from_string(operator_id)
        self._operator_key = PrivateKey.from_string(operator_key)
        self._client.set_operator(self._operator_id, self._operator_key)

    def get_balance(self, account_id: str) -> float:
        """
        Query account balance from Hedera network.

        Args:
            account_id: Account ID to query (e.g., '0.0.12345')

        Returns:
            Balance in hbars
        """
        aid = AccountId.from_string(account_id)
        balance = CryptoGetAccountBalanceQuery(account_id=aid).execute(self._client)
        return balance.hbars

    def close(self):
        """Close the client connection."""
        self._client.close()


class Runner:
    """Manages benchmark load generation."""

    def __init__(
        self,
        client: HederaSDKClient,
        account_ids: list[str],
        concurrency: int,
    ):
        self.client = client
        self.account_ids = account_ids
        self.concurrency = concurrency
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
                future.result()

        self.results.set_end_time(datetime.now())


def load_hedera_account_ids(count: int, base_account: str) -> list[str]:
    """
    Generate a list of Hedera account IDs to query.

    For testnet benchmarking, we query a range of accounts around the base account.
    Many will return zero balance, but that's fine for latency measurement.

    Args:
        count: Number of account IDs to generate
        base_account: Base account ID (e.g., '0.0.12345')

    Returns:
        List of account IDs
    """
    parts = base_account.split('.')
    shard = int(parts[0])
    realm = int(parts[1])
    base_num = int(parts[2])

    account_ids = []
    for i in range(count):
        # Spread accounts around the base number
        offset = i - count // 2
        num = max(1, base_num + offset)
        account_ids.append(f"{shard}.{realm}.{num}")

    return account_ids


def main():
    parser = argparse.ArgumentParser(
        description="Python Hedera SDK benchmark client",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Environment variables:
  HEDERA_OPERATOR_ID    Operator account ID (required)
  HEDERA_OPERATOR_KEY   Operator private key (required)

Example:
  export HEDERA_OPERATOR_ID="0.0.12345"
  export HEDERA_OPERATOR_KEY="302e020100300506..."
  python sdk_client.py --network=testnet --duration=30
        """
    )
    parser.add_argument("--network", default="testnet",
                        choices=["testnet", "mainnet", "previewnet"],
                        help="Hedera network to connect to")
    parser.add_argument("--concurrency", type=int, default=5,
                        help="Number of parallel workers (default: 5, keep low to avoid rate limits)")
    parser.add_argument("--duration", type=parse_duration, default=30,
                        help="Test duration (e.g., 30, 30s, 1m)")
    parser.add_argument("--account-count", type=int, default=100,
                        help="Number of accounts to query (spread around operator ID)")

    # Database flags for storing results
    parser.add_argument("--db-host", default="localhost")
    parser.add_argument("--db-port", type=int, default=5432)
    parser.add_argument("--db-user", default="benchmark")
    parser.add_argument("--db-pass", default="benchmark_pass")
    parser.add_argument("--db-name", default="grpc_benchmark")
    parser.add_argument("--skip-db", action="store_true",
                        help="Skip storing results in database")

    args = parser.parse_args()

    # Load .env file if present (check multiple locations)
    env_paths = [
        Path.cwd() / ".env",
        Path(__file__).parent / ".env",
        Path.cwd() / "clients" / "python" / ".env",
    ]
    for env_path in env_paths:
        if env_path.exists():
            load_dotenv(env_path)
            print(f"Loaded credentials from {env_path}")
            break

    # Load operator credentials from environment
    operator_id = os.environ.get("HEDERA_OPERATOR_ID")
    operator_key = os.environ.get("HEDERA_OPERATOR_KEY")

    if not operator_id or not operator_key:
        print("Error: HEDERA_OPERATOR_ID and HEDERA_OPERATOR_KEY environment variables required")
        print("\nGet free testnet credentials at: https://portal.hedera.com/")
        print("Then export them:")
        print('  export HEDERA_OPERATOR_ID="0.0.xxxxx"')
        print('  export HEDERA_OPERATOR_KEY="302e020100300506..."')
        sys.exit(1)

    # Connect to database (optional)
    db = None
    if not args.skip_db:
        try:
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
        except Exception as e:
            print(f"Warning: Could not connect to database: {e}")
            print("Results will not be persisted. Use --skip-db to suppress this warning.")

    # Generate account IDs to query
    print(f"Generating {args.account_count} account IDs around {operator_id}...")
    account_ids = load_hedera_account_ids(args.account_count, operator_id)

    # Create Hedera SDK client
    print(f"Connecting to Hedera {args.network}...")
    try:
        client = HederaSDKClient(args.network, operator_id, operator_key)
    except Exception as e:
        print(f"Error initializing Hedera SDK client: {e}")
        sys.exit(1)
    print(f"Connected to Hedera {args.network} as {operator_id}")

    # Verify connectivity with a test query
    print("Verifying connectivity with test balance query...")
    try:
        balance = client.get_balance(operator_id)
        print(f"Operator balance: {balance} HBAR")
    except Exception as e:
        print(f"Error querying balance: {e}")
        print("Check your credentials and network connectivity.")
        sys.exit(1)

    # Create runner
    runner = Runner(client, account_ids, args.concurrency)

    # Handle interrupt signals
    def signal_handler(sig, frame):
        print("\nReceived interrupt signal, stopping benchmark...")
        runner.stop()

    signal.signal(signal.SIGINT, signal_handler)
    signal.signal(signal.SIGTERM, signal_handler)

    # Run benchmark
    print(f"\nStarting balance benchmark (hedera-sdk protocol, python client)")
    print(f"Network: {args.network} | Concurrency: {args.concurrency} | Duration: {args.duration}s")
    print(f"Note: Hedera testnet has rate limits; high concurrency may cause errors\n")

    # Start resource monitoring
    resource_monitor = ResourceMonitor(interval_ms=100)
    stop_monitor = resource_monitor.start()

    runner.run_balance(args.duration)

    # Stop resource monitoring and record stats
    resource_stats = stop_monitor()
    runner.results.set_resource_stats(resource_stats)

    # Print and store results
    runner.results.print_summary("balance", "hedera-sdk", args.concurrency)

    if db:
        runner.results.store_results(
            db,
            scenario="balance",
            protocol="hedera-sdk",
            client="python-sdk",
            concurrency=args.concurrency,
            rate_limit=None,
        )
        db.close()

    client.close()


if __name__ == "__main__":
    main()
