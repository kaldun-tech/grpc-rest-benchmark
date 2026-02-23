package db

import (
	"context"
	"testing"
	"time"
)

func TestGetTransactions(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test basic query with limit
	opts := StreamTransactionsOptions{
		Limit: 10,
	}
	txs, err := db.GetTransactions(ctx, opts)
	if err != nil {
		t.Fatalf("GetTransactions() error = %v", err)
	}

	if len(txs) == 0 {
		t.Error("GetTransactions() returned empty slice")
	}
	if len(txs) > 10 {
		t.Errorf("GetTransactions() returned %d transactions, want <= 10", len(txs))
	}

	// Verify transaction fields
	for _, tx := range txs {
		if tx.TxID == "" {
			t.Error("Transaction TxID is empty")
		}
		if tx.FromAccount == "" {
			t.Error("Transaction FromAccount is empty")
		}
		if tx.ToAccount == "" {
			t.Error("Transaction ToAccount is empty")
		}
		if tx.TxType == "" {
			t.Error("Transaction TxType is empty")
		}
		if tx.Timestamp.IsZero() {
			t.Error("Transaction Timestamp is zero")
		}
	}
}

func TestGetTransactions_WithFilter(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// First get an account that has transactions
	accountID, err := db.GetRandomAccountID(ctx)
	if err != nil {
		t.Fatalf("GetRandomAccountID() error = %v", err)
	}

	opts := StreamTransactionsOptions{
		FilterAccount: accountID,
		Limit:         100,
	}
	txs, err := db.GetTransactions(ctx, opts)
	if err != nil {
		t.Fatalf("GetTransactions() with filter error = %v", err)
	}

	// Verify all returned transactions involve the filtered account
	for _, tx := range txs {
		if tx.FromAccount != accountID && tx.ToAccount != accountID {
			t.Errorf("Transaction %s does not involve account %s", tx.TxID, accountID)
		}
	}
}

func TestGetTransactions_OrderedByTimestamp(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	opts := StreamTransactionsOptions{
		Limit: 100,
	}
	txs, err := db.GetTransactions(ctx, opts)
	if err != nil {
		t.Fatalf("GetTransactions() error = %v", err)
	}

	// Verify transactions are in ascending timestamp order
	for i := 1; i < len(txs); i++ {
		if txs[i].Timestamp.Before(txs[i-1].Timestamp) {
			t.Errorf("Transactions not in order: %v > %v", txs[i-1].Timestamp, txs[i].Timestamp)
		}
	}
}

func TestStreamTransactions(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	opts := StreamTransactionsOptions{
		Limit: 10,
	}
	txCh, errCh := db.StreamTransactions(ctx, opts)

	var txs []*Transaction
	for tx := range txCh {
		txs = append(txs, tx)
	}

	// Check for errors
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("StreamTransactions() error = %v", err)
		}
	default:
	}

	if len(txs) == 0 {
		t.Error("StreamTransactions() yielded no transactions")
	}
	if len(txs) > 10 {
		t.Errorf("StreamTransactions() yielded %d transactions, want <= 10", len(txs))
	}
}

func TestStreamTransactions_Cancellation(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx, cancel := context.WithCancel(context.Background())

	opts := StreamTransactionsOptions{
		Limit: 1000, // Request many transactions
	}
	txCh, errCh := db.StreamTransactions(ctx, opts)

	// Read a few then cancel
	count := 0
	for range txCh {
		count++
		if count >= 5 {
			cancel()
			break
		}
	}

	// Drain remaining (should stop quickly)
	for range txCh {
	}

	// Check that we got context cancellation error
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Logf("StreamTransactions() error after cancel = %v (expected context.Canceled)", err)
		}
	default:
	}

	if count < 5 {
		t.Errorf("Only received %d transactions before cancel", count)
	}
}

func TestGetTransactionCount(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	count, err := db.GetTransactionCount(ctx)
	if err != nil {
		t.Fatalf("GetTransactionCount() error = %v", err)
	}

	if count <= 0 {
		t.Errorf("GetTransactionCount() = %d, want > 0", count)
	}
}
