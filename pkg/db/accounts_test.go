package db

import (
	"context"
	"testing"
	"time"
)

func TestGetBalance(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First get a valid account ID
	accountID, err := db.GetRandomAccountID(ctx)
	if err != nil {
		t.Fatalf("GetRandomAccountID() error = %v", err)
	}

	// Test GetBalance with valid account
	acc, err := db.GetBalance(ctx, accountID)
	if err != nil {
		t.Fatalf("GetBalance(%q) error = %v", accountID, err)
	}

	if acc.AccountID != accountID {
		t.Errorf("AccountID = %q, want %q", acc.AccountID, accountID)
	}
	if acc.Balance < 0 {
		t.Errorf("Balance = %d, want non-negative", acc.Balance)
	}
	if acc.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should not be zero")
	}
}

func TestGetBalance_NotFound(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test with non-existent account
	_, err := db.GetBalance(ctx, "0.0.999999999")
	if err == nil {
		t.Error("GetBalance() expected error for non-existent account, got nil")
	}
}

func TestGetBalances(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get some account IDs first
	allIDs, err := db.GetAllAccountIDs(ctx)
	if err != nil {
		t.Fatalf("GetAllAccountIDs() error = %v", err)
	}
	if len(allIDs) < 3 {
		t.Skip("Need at least 3 accounts for this test")
	}

	// Test GetBalances with multiple accounts
	testIDs := allIDs[:3]
	accounts, err := db.GetBalances(ctx, testIDs)
	if err != nil {
		t.Fatalf("GetBalances() error = %v", err)
	}

	if len(accounts) != 3 {
		t.Errorf("GetBalances() returned %d accounts, want 3", len(accounts))
	}

	// Verify all returned accounts are in our request
	idSet := make(map[string]bool)
	for _, id := range testIDs {
		idSet[id] = true
	}
	for _, acc := range accounts {
		if !idSet[acc.AccountID] {
			t.Errorf("Unexpected account ID %q in results", acc.AccountID)
		}
	}
}

func TestGetBalances_Empty(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test with empty slice
	accounts, err := db.GetBalances(ctx, []string{})
	if err != nil {
		t.Fatalf("GetBalances([]) error = %v", err)
	}
	if len(accounts) != 0 {
		t.Errorf("GetBalances([]) returned %d accounts, want 0", len(accounts))
	}
}

func TestGetRandomAccountID(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Call multiple times to verify randomness
	ids := make(map[string]int)
	for i := 0; i < 10; i++ {
		id, err := db.GetRandomAccountID(ctx)
		if err != nil {
			t.Fatalf("GetRandomAccountID() error = %v", err)
		}
		if id == "" {
			t.Error("GetRandomAccountID() returned empty string")
		}
		ids[id]++
	}

	// With 10K accounts, getting the same ID 10 times would be very unlikely
	// This is a weak test but verifies basic functionality
	if len(ids) < 2 {
		t.Log("Warning: GetRandomAccountID returned same ID multiple times (might be OK with small dataset)")
	}
}

func TestGetAccountCount(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	count, err := db.GetAccountCount(ctx)
	if err != nil {
		t.Fatalf("GetAccountCount() error = %v", err)
	}

	if count <= 0 {
		t.Errorf("GetAccountCount() = %d, want > 0", count)
	}
}

func TestGetAllAccountIDs(t *testing.T) {
	db := testDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ids, err := db.GetAllAccountIDs(ctx)
	if err != nil {
		t.Fatalf("GetAllAccountIDs() error = %v", err)
	}

	if len(ids) == 0 {
		t.Error("GetAllAccountIDs() returned empty slice")
	}

	// Verify count matches
	count, err := db.GetAccountCount(ctx)
	if err != nil {
		t.Fatalf("GetAccountCount() error = %v", err)
	}

	if int64(len(ids)) != count {
		t.Errorf("GetAllAccountIDs() returned %d IDs, GetAccountCount() returned %d", len(ids), count)
	}

	// Verify no duplicates
	seen := make(map[string]bool)
	for _, id := range ids {
		if seen[id] {
			t.Errorf("Duplicate account ID: %q", id)
		}
		seen[id] = true
	}
}
