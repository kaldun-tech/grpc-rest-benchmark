package db

import (
	"context"
	"fmt"
	"time"
)

// Transaction represents a transaction record.
type Transaction struct {
	TxID        string
	FromAccount string
	ToAccount   string
	Amount      int64
	TxType      string
	Timestamp   time.Time
}

// StreamTransactionsOptions configures transaction streaming.
type StreamTransactionsOptions struct {
	Since         time.Time // Start from this timestamp (zero = beginning)
	FilterAccount string    // Filter by account (empty = all)
	Limit         int       // Max transactions to return (0 = no limit)
}

// StreamTransactions retrieves transactions for streaming.
// Returns a channel that yields transactions in timestamp order.
func (db *DB) StreamTransactions(ctx context.Context, opts StreamTransactionsOptions) (<-chan *Transaction, <-chan error) {
	txCh := make(chan *Transaction, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(txCh)
		defer close(errCh)

		query := `SELECT tx_id, from_account, to_account, amount_tinybar, tx_type, timestamp
				  FROM transactions
				  WHERE ($1::timestamp IS NULL OR timestamp >= $1)
				    AND ($2 = '' OR from_account = $2 OR to_account = $2)
				  ORDER BY timestamp ASC`

		var since *time.Time
		if !opts.Since.IsZero() {
			since = &opts.Since
		}

		rows, err := db.Pool.Query(ctx, query, since, opts.FilterAccount)
		if err != nil {
			errCh <- fmt.Errorf("failed to query transactions: %w", err)
			return
		}
		defer rows.Close()

		count := 0
		for rows.Next() {
			if opts.Limit > 0 && count >= opts.Limit {
				break
			}

			var tx Transaction
			if err := rows.Scan(&tx.TxID, &tx.FromAccount, &tx.ToAccount, &tx.Amount, &tx.TxType, &tx.Timestamp); err != nil {
				errCh <- fmt.Errorf("failed to scan transaction row: %w", err)
				return
			}

			select {
			case txCh <- &tx:
				count++
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}

		if err := rows.Err(); err != nil {
			errCh <- fmt.Errorf("error iterating transaction rows: %w", err)
		}
	}()

	return txCh, errCh
}

// GetTransactions retrieves transactions synchronously (for simpler use cases).
func (db *DB) GetTransactions(ctx context.Context, opts StreamTransactionsOptions) ([]*Transaction, error) {
	query := `SELECT tx_id, from_account, to_account, amount_tinybar, tx_type, timestamp
			  FROM transactions
			  WHERE ($1::timestamp IS NULL OR timestamp >= $1)
			    AND ($2 = '' OR from_account = $2 OR to_account = $2)
			  ORDER BY timestamp ASC`

	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}

	var since *time.Time
	if !opts.Since.IsZero() {
		since = &opts.Since
	}

	rows, err := db.Pool.Query(ctx, query, since, opts.FilterAccount)
	if err != nil {
		return nil, fmt.Errorf("failed to query transactions: %w", err)
	}
	defer rows.Close()

	var transactions []*Transaction
	for rows.Next() {
		var tx Transaction
		if err := rows.Scan(&tx.TxID, &tx.FromAccount, &tx.ToAccount, &tx.Amount, &tx.TxType, &tx.Timestamp); err != nil {
			return nil, fmt.Errorf("failed to scan transaction row: %w", err)
		}
		transactions = append(transactions, &tx)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating transaction rows: %w", err)
	}

	return transactions, nil
}

// GetTransactionCount returns the total number of transactions.
func (db *DB) GetTransactionCount(ctx context.Context) (int64, error) {
	var count int64
	err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM transactions`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get transaction count: %w", err)
	}
	return count, nil
}
