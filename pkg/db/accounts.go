package db

import (
	"context"
	"fmt"
	"time"
)

// Account represents an account balance record.
type Account struct {
	AccountID   string
	Balance     int64
	UpdatedAt   time.Time
}

// GetBalance retrieves the balance for a single account.
func (db *DB) GetBalance(ctx context.Context, accountID string) (*Account, error) {
	var acc Account
	err := db.Pool.QueryRow(ctx,
		`SELECT account_id, balance_tinybar, updated_at
		 FROM accounts
		 WHERE account_id = $1`,
		accountID,
	).Scan(&acc.AccountID, &acc.Balance, &acc.UpdatedAt)

	if err != nil {
		return nil, fmt.Errorf("failed to get balance for %s: %w", accountID, err)
	}

	return &acc, nil
}

// GetBalances retrieves balances for multiple accounts.
func (db *DB) GetBalances(ctx context.Context, accountIDs []string) ([]*Account, error) {
	if len(accountIDs) == 0 {
		return []*Account{}, nil
	}

	rows, err := db.Pool.Query(ctx,
		`SELECT account_id, balance_tinybar, updated_at
		 FROM accounts
		 WHERE account_id = ANY($1)`,
		accountIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get balances: %w", err)
	}
	defer rows.Close()

	accounts := make([]*Account, 0, len(accountIDs))
	for rows.Next() {
		var acc Account
		if err := rows.Scan(&acc.AccountID, &acc.Balance, &acc.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan account row: %w", err)
		}
		accounts = append(accounts, &acc)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating account rows: %w", err)
	}

	return accounts, nil
}

// GetRandomAccountID returns a random account ID from the database.
// Useful for benchmark load generation.
func (db *DB) GetRandomAccountID(ctx context.Context) (string, error) {
	var accountID string
	err := db.Pool.QueryRow(ctx,
		`SELECT account_id FROM accounts ORDER BY RANDOM() LIMIT 1`,
	).Scan(&accountID)

	if err != nil {
		return "", fmt.Errorf("failed to get random account: %w", err)
	}

	return accountID, nil
}

// GetAccountCount returns the total number of accounts.
func (db *DB) GetAccountCount(ctx context.Context) (int64, error) {
	var count int64
	err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounts`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get account count: %w", err)
	}
	return count, nil
}

// GetAllAccountIDs returns all account IDs from the database.
// Used to pre-load account IDs for benchmarking.
func (db *DB) GetAllAccountIDs(ctx context.Context) ([]string, error) {
	rows, err := db.Pool.Query(ctx, `SELECT account_id FROM accounts`)
	if err != nil {
		return nil, fmt.Errorf("failed to query account IDs: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan account ID: %w", err)
		}
		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating account IDs: %w", err)
	}

	return ids, nil
}
