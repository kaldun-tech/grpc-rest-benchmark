"""
Resource monitoring for Python benchmark clients.

Uses psutil to track CPU and memory usage during benchmark execution.
"""

import os
import threading
import time
from dataclasses import dataclass
from typing import Callable, Optional

import psutil


@dataclass
class ResourceStats:
    """Aggregated resource usage metrics."""
    cpu_avg_percent: float
    memory_avg_mb: float
    memory_peak_mb: float
    sample_count: int


class ResourceMonitor:
    """
    Samples CPU and memory usage during benchmark execution.

    Usage:
        monitor = ResourceMonitor(interval_ms=100)
        stop_fn = monitor.start()

        # ... run benchmark ...

        stats = stop_fn()
        print(f"CPU: {stats.cpu_avg_percent}%")
    """

    def __init__(self, interval_ms: int = 100):
        """
        Initialize resource monitor.

        Args:
            interval_ms: Sampling interval in milliseconds
        """
        self._interval = interval_ms / 1000.0
        self._process = psutil.Process(os.getpid())

        self._lock = threading.Lock()
        self._cpu_samples: list[float] = []
        self._mem_samples: list[float] = []
        self._mem_peak: float = 0.0
        self._sample_count: int = 0

        self._stop_event = threading.Event()
        self._thread: Optional[threading.Thread] = None

    def start(self) -> Callable[[], ResourceStats]:
        """
        Begin collecting resource samples in the background.

        Returns:
            A stop function that returns the collected ResourceStats when called.
        """
        self._stop_event.clear()
        self._thread = threading.Thread(target=self._sample_loop, daemon=True)
        self._thread.start()

        return self._stop_and_get_stats

    def _sample_loop(self):
        """Background sampling loop."""
        while not self._stop_event.is_set():
            self._sample()
            time.sleep(self._interval)

    def _sample(self):
        """Take a single CPU/memory sample."""
        with self._lock:
            try:
                # CPU percent since last call (first call may be 0)
                cpu_percent = self._process.cpu_percent()
                self._cpu_samples.append(cpu_percent)
            except (psutil.NoSuchProcess, psutil.AccessDenied):
                pass

            try:
                # Memory in MB
                mem_info = self._process.memory_info()
                mem_mb = mem_info.rss / (1024 * 1024)
                self._mem_samples.append(mem_mb)
                if mem_mb > self._mem_peak:
                    self._mem_peak = mem_mb
            except (psutil.NoSuchProcess, psutil.AccessDenied):
                pass

            self._sample_count += 1

    def _stop_and_get_stats(self) -> ResourceStats:
        """Stop monitoring and return aggregated statistics."""
        self._stop_event.set()
        if self._thread:
            self._thread.join(timeout=1.0)

        with self._lock:
            cpu_avg = 0.0
            if self._cpu_samples:
                # Skip first sample as it may be inaccurate
                samples = self._cpu_samples[1:] if len(self._cpu_samples) > 1 else self._cpu_samples
                cpu_avg = sum(samples) / len(samples) if samples else 0.0

            mem_avg = 0.0
            if self._mem_samples:
                mem_avg = sum(self._mem_samples) / len(self._mem_samples)

            return ResourceStats(
                cpu_avg_percent=cpu_avg,
                memory_avg_mb=mem_avg,
                memory_peak_mb=self._mem_peak,
                sample_count=self._sample_count,
            )
