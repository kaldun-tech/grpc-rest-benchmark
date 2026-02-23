#!/usr/bin/env python3
"""
Fetch HCS (Hedera Consensus Service) topic messages and extract timing distribution.

This script pulls messages from a public HCS topic via the Hedera Mirror Node REST API
and calculates inter-arrival times to create realistic workload replay patterns.

Usage:
    python scripts/fetch_hcs_timing.py --topic 0.0.120438 --limit 1000 --output timing.json
    python scripts/fetch_hcs_timing.py --topic 0.0.3948064 --network testnet --limit 500

Output format (JSON):
{
    "topic_id": "0.0.120438",
    "network": "mainnet",
    "message_count": 1000,
    "time_span_seconds": 3600.5,
    "avg_rate_per_second": 0.28,
    "inter_arrival_ms": [150, 200, 50, ...],
    "stats": {
        "min_ms": 10,
        "max_ms": 5000,
        "avg_ms": 250,
        "p50_ms": 200,
        "p90_ms": 500,
        "p99_ms": 2000
    }
}
"""

import argparse
import json
import sys
import time
from dataclasses import dataclass, asdict
from typing import Optional
from urllib.request import urlopen, Request
from urllib.error import URLError, HTTPError


MIRROR_NODES = {
    "mainnet": "https://mainnet-public.mirrornode.hedera.com",
    "testnet": "https://testnet.mirrornode.hedera.com",
    "previewnet": "https://previewnet.mirrornode.hedera.com",
}


@dataclass
class TimingStats:
    min_ms: float
    max_ms: float
    avg_ms: float
    p50_ms: float
    p90_ms: float
    p99_ms: float


@dataclass
class TimingData:
    topic_id: str
    network: str
    message_count: int
    time_span_seconds: float
    avg_rate_per_second: float
    inter_arrival_ms: list[float]
    stats: TimingStats


def parse_consensus_timestamp(ts: str) -> float:
    """Parse consensus timestamp (seconds.nanoseconds) to float seconds."""
    parts = ts.split(".")
    seconds = int(parts[0])
    nanos = int(parts[1]) if len(parts) > 1 else 0
    return seconds + nanos / 1e9


def fetch_messages(base_url: str, topic_id: str, limit: int, delay_ms: int = 100) -> list[dict]:
    """Fetch messages from HCS topic with pagination."""
    messages = []
    url = f"{base_url}/api/v1/topics/{topic_id}/messages?limit=100&order=asc"

    while url and len(messages) < limit:
        try:
            req = Request(url, headers={"Accept": "application/json"})
            with urlopen(req, timeout=30) as response:
                data = json.loads(response.read().decode())
        except HTTPError as e:
            print(f"HTTP error {e.code}: {e.reason}", file=sys.stderr)
            if e.code == 404:
                print(f"Topic {topic_id} not found or has no messages", file=sys.stderr)
            break
        except URLError as e:
            print(f"URL error: {e.reason}", file=sys.stderr)
            break
        except json.JSONDecodeError as e:
            print(f"JSON decode error: {e}", file=sys.stderr)
            break

        batch = data.get("messages", [])
        if not batch:
            break

        messages.extend(batch)
        print(f"  Fetched {len(messages)} messages...", file=sys.stderr)

        # Get next page URL
        links = data.get("links", {})
        next_path = links.get("next")
        if next_path:
            url = f"{base_url}{next_path}"
            time.sleep(delay_ms / 1000)  # Rate limit
        else:
            url = None

    return messages[:limit]


def calculate_inter_arrivals(messages: list[dict]) -> list[float]:
    """Calculate inter-arrival times in milliseconds from message timestamps."""
    if len(messages) < 2:
        return []

    timestamps = [parse_consensus_timestamp(m["consensus_timestamp"]) for m in messages]
    timestamps.sort()

    inter_arrivals = []
    for i in range(1, len(timestamps)):
        delta_ms = (timestamps[i] - timestamps[i - 1]) * 1000
        inter_arrivals.append(round(delta_ms, 3))

    return inter_arrivals


def calculate_stats(inter_arrivals: list[float]) -> TimingStats:
    """Calculate statistics for inter-arrival times."""
    if not inter_arrivals:
        return TimingStats(0, 0, 0, 0, 0, 0)

    sorted_vals = sorted(inter_arrivals)
    n = len(sorted_vals)

    def percentile(p: float) -> float:
        idx = int(p * n)
        return sorted_vals[min(idx, n - 1)]

    return TimingStats(
        min_ms=round(min(sorted_vals), 3),
        max_ms=round(max(sorted_vals), 3),
        avg_ms=round(sum(sorted_vals) / n, 3),
        p50_ms=round(percentile(0.50), 3),
        p90_ms=round(percentile(0.90), 3),
        p99_ms=round(percentile(0.99), 3),
    )


def process_topic(
    topic_id: str,
    network: str = "mainnet",
    limit: int = 1000,
    delay_ms: int = 100,
) -> Optional[TimingData]:
    """Fetch and process HCS topic messages."""
    base_url = MIRROR_NODES.get(network)
    if not base_url:
        print(f"Unknown network: {network}", file=sys.stderr)
        return None

    print(f"Fetching messages from topic {topic_id} on {network}...", file=sys.stderr)
    messages = fetch_messages(base_url, topic_id, limit, delay_ms)

    if len(messages) < 2:
        print(f"Not enough messages ({len(messages)}) to calculate timing", file=sys.stderr)
        return None

    print(f"Processing {len(messages)} messages...", file=sys.stderr)

    inter_arrivals = calculate_inter_arrivals(messages)
    stats = calculate_stats(inter_arrivals)

    # Calculate time span
    timestamps = [parse_consensus_timestamp(m["consensus_timestamp"]) for m in messages]
    time_span = max(timestamps) - min(timestamps)

    return TimingData(
        topic_id=topic_id,
        network=network,
        message_count=len(messages),
        time_span_seconds=round(time_span, 3),
        avg_rate_per_second=round(len(messages) / time_span, 3) if time_span > 0 else 0,
        inter_arrival_ms=inter_arrivals,
        stats=stats,
    )


def main():
    parser = argparse.ArgumentParser(
        description="Fetch HCS topic messages and extract timing distribution"
    )
    parser.add_argument(
        "--topic",
        required=True,
        help="HCS topic ID (e.g., 0.0.120438)",
    )
    parser.add_argument(
        "--network",
        choices=["mainnet", "testnet", "previewnet"],
        default="mainnet",
        help="Hedera network (default: mainnet)",
    )
    parser.add_argument(
        "--limit",
        type=int,
        default=1000,
        help="Maximum number of messages to fetch (default: 1000)",
    )
    parser.add_argument(
        "--output",
        "-o",
        help="Output file path (default: stdout)",
    )
    parser.add_argument(
        "--delay",
        type=int,
        default=100,
        help="Delay between API requests in ms (default: 100)",
    )

    args = parser.parse_args()

    timing_data = process_topic(
        topic_id=args.topic,
        network=args.network,
        limit=args.limit,
        delay_ms=args.delay,
    )

    if timing_data is None:
        sys.exit(1)

    # Convert to dict for JSON serialization
    output = asdict(timing_data)
    json_str = json.dumps(output, indent=2)

    if args.output:
        with open(args.output, "w") as f:
            f.write(json_str)
        print(f"Wrote timing data to {args.output}", file=sys.stderr)
    else:
        print(json_str)

    # Print summary
    print(f"\nSummary:", file=sys.stderr)
    print(f"  Messages: {timing_data.message_count}", file=sys.stderr)
    print(f"  Time span: {timing_data.time_span_seconds:.1f}s", file=sys.stderr)
    print(f"  Avg rate: {timing_data.avg_rate_per_second:.2f} msg/s", file=sys.stderr)
    print(f"  Inter-arrival p50: {timing_data.stats.p50_ms:.1f}ms", file=sys.stderr)
    print(f"  Inter-arrival p99: {timing_data.stats.p99_ms:.1f}ms", file=sys.stderr)


if __name__ == "__main__":
    main()
