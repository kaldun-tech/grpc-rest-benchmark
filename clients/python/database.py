"""
Database connection pooling and retry logic for Python benchmark clients.
"""

import time
from dataclasses import dataclass
from typing import Optional

import psycopg
from psycopg_pool import ConnectionPool


@dataclass
class DBConfig:
    """Database connection configuration."""
    host: str = "localhost"
    port: int = 5432
    user: str = "benchmark"
    password: str = "benchmark_pass"
    database: str = "grpc_benchmark"

    # Pool configuration
    min_size: int = 5
    max_size: int = 50
    max_lifetime: float = 3600.0  # 1 hour in seconds
    max_idle: float = 1800.0  # 30 minutes in seconds

    # Retry configuration
    max_retries: int = 3
    retry_interval: float = 0.1  # 100ms initial interval

    def connection_string(self) -> str:
        return f"host={self.host} port={self.port} user={self.user} password={self.password} dbname={self.database}"


class Database:
    """Database connection pool with retry logic."""

    def __init__(self, config: Optional[DBConfig] = None):
        self.config = config or DBConfig()
        self._pool: Optional[ConnectionPool] = None

    def connect(self) -> None:
        """Connect to database with retry logic."""
        last_error = None
        retry_interval = self.config.retry_interval

        for attempt in range(self.config.max_retries + 1):
            if attempt > 0:
                time.sleep(retry_interval)
                retry_interval *= 2  # Exponential backoff

            try:
                self._pool = ConnectionPool(
                    self.config.connection_string(),
                    min_size=self.config.min_size,
                    max_size=self.config.max_size,
                    max_lifetime=self.config.max_lifetime,
                    max_idle=self.config.max_idle,
                    open=True,  # Open pool immediately
                )
                # Verify connectivity
                with self._pool.connection() as conn:
                    conn.execute("SELECT 1")
                return
            except Exception as e:
                last_error = e
                if self._pool:
                    self._pool.close()
                    self._pool = None

        raise ConnectionError(
            f"Failed to connect after {self.config.max_retries} retries: {last_error}"
        )

    def get_connection(self) -> psycopg.Connection:
        """Get a connection from the pool."""
        if not self._pool:
            raise RuntimeError("Database not connected. Call connect() first.")
        return self._pool.connection()

    def execute(self, query: str, params: tuple = ()) -> list:
        """Execute a query and return results."""
        with self.get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute(query, params)
                if cur.description:
                    return cur.fetchall()
                return []

    def execute_one(self, query: str, params: tuple = ()):
        """Execute a query and return single result."""
        with self.get_connection() as conn:
            with conn.cursor() as cur:
                cur.execute(query, params)
                return cur.fetchone()

    def execute_many(self, query: str, params_list: list[tuple]) -> None:
        """Execute a query with multiple parameter sets."""
        with self.get_connection() as conn:
            with conn.cursor() as cur:
                cur.executemany(query, params_list)
            conn.commit()

    def close(self) -> None:
        """Close the connection pool."""
        if self._pool:
            self._pool.close()
            self._pool = None

    @property
    def pool_stats(self) -> dict:
        """Get pool statistics."""
        if not self._pool:
            return {}
        return {
            "pool_size": self._pool.get_stats().get("pool_size", 0),
            "pool_available": self._pool.get_stats().get("pool_available", 0),
            "requests_waiting": self._pool.get_stats().get("requests_waiting", 0),
        }


def load_account_ids(db: Database) -> list[str]:
    """Load all account IDs from the database."""
    rows = db.execute("SELECT account_id FROM accounts")
    return [row[0] for row in rows]
