package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/kaldun-tech/grpc-rest-benchmark/pkg/db"
)

var (
	port     = flag.Int("port", 8080, "REST server port")
	dbHost   = flag.String("db-host", "localhost", "PostgreSQL host")
	dbPort   = flag.Int("db-port", 5432, "PostgreSQL port")
	dbUser   = flag.String("db-user", "benchmark", "PostgreSQL user")
	dbPass   = flag.String("db-pass", "benchmark_pass", "PostgreSQL password")
	dbName   = flag.String("db-name", "grpc_benchmark", "PostgreSQL database")
)

// Server holds the REST server state.
type Server struct {
	db *db.DB
}

// BalanceResponse is the JSON response for balance queries.
type BalanceResponse struct {
	Account   string `json:"account"`
	Balance   int64  `json:"balance"`
	Timestamp string `json:"timestamp"`
}

// BatchBalanceResponse is the JSON response for batch balance queries.
type BatchBalanceResponse struct {
	Balances []BalanceResponse `json:"balances"`
}

// TransactionEvent is the JSON payload for SSE transaction events.
type TransactionEvent struct {
	TxID      string `json:"tx_id"`
	From      string `json:"from"`
	To        string `json:"to"`
	Amount    int64  `json:"amount"`
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
}

// ErrorResponse is the JSON response for errors.
type ErrorResponse struct {
	Error string `json:"error"`
}

func main() {
	flag.Parse()

	// Setup database connection
	ctx := context.Background()
	dbCfg := db.Config{
		Host:     *dbHost,
		Port:     *dbPort,
		User:     *dbUser,
		Password: *dbPass,
		Database: *dbName,
	}

	database, err := db.New(ctx, dbCfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close()
	log.Printf("Connected to database %s@%s:%d", dbCfg.Database, dbCfg.Host, dbCfg.Port)

	server := &Server{db: database}

	// Setup routes
	mux := http.NewServeMux()

	// Balance endpoints
	mux.HandleFunc("/api/v1/accounts/", server.handleAccountBalance)
	mux.HandleFunc("/api/v1/balances", server.handleBatchBalances)

	// Transaction streaming
	mux.HandleFunc("/api/v1/transactions/stream", server.handleTransactionStream)

	// Health check
	mux.HandleFunc("/health", server.handleHealth)

	// Create HTTP server
	addr := fmt.Sprintf(":%d", *port)
	httpServer := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0, // Disabled for SSE
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down REST server...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpServer.Shutdown(ctx)
	}()

	log.Printf("REST server listening on %s", addr)
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Failed to serve: %v", err)
	}
}

// handleAccountBalance handles GET /api/v1/accounts/{id}/balance
func (s *Server) handleAccountBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Parse account ID from path: /api/v1/accounts/{id}/balance
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/accounts/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 || parts[0] == "" {
		writeError(w, http.StatusBadRequest, "Account ID required")
		return
	}
	accountID := parts[0]

	account, err := s.db.GetBalance(r.Context(), accountID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Account not found: %v", err))
		return
	}

	resp := BalanceResponse{
		Account:   account.AccountID,
		Balance:   account.Balance,
		Timestamp: account.UpdatedAt.Format(time.RFC3339),
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleBatchBalances handles GET /api/v1/balances?ids=0.0.123,0.0.456
func (s *Server) handleBatchBalances(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	idsParam := r.URL.Query().Get("ids")
	if idsParam == "" {
		writeError(w, http.StatusBadRequest, "ids parameter required")
		return
	}

	accountIDs := strings.Split(idsParam, ",")
	accounts, err := s.db.GetBalances(r.Context(), accountIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to get balances: %v", err))
		return
	}

	balances := make([]BalanceResponse, len(accounts))
	for i, acc := range accounts {
		balances[i] = BalanceResponse{
			Account:   acc.AccountID,
			Balance:   acc.Balance,
			Timestamp: acc.UpdatedAt.Format(time.RFC3339),
		}
	}

	writeJSON(w, http.StatusOK, BatchBalanceResponse{Balances: balances})
}

// handleTransactionStream handles GET /api/v1/transactions/stream (SSE)
func (s *Server) handleTransactionStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	// Parse query parameters
	sinceParam := r.URL.Query().Get("since")
	var since time.Time
	if sinceParam != "" {
		var err error
		since, err = time.Parse(time.RFC3339, sinceParam)
		if err != nil {
			since = time.Time{}
		}
	}

	filterAccount := r.URL.Query().Get("account")

	rateLimit := 0
	if rl := r.URL.Query().Get("rate"); rl != "" {
		fmt.Sscanf(rl, "%d", &rateLimit)
	}

	opts := db.StreamTransactionsOptions{
		Since:         since,
		FilterAccount: filterAccount,
	}

	ctx := r.Context()
	txCh, errCh := s.db.StreamTransactions(ctx, opts)

	// Rate limiting
	var ticker *time.Ticker
	if rateLimit > 0 {
		ticker = time.NewTicker(time.Second / time.Duration(rateLimit))
		defer ticker.Stop()
	}

	for tx := range txCh {
		// Apply rate limiting if configured
		if ticker != nil {
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return
			}
		}

		event := TransactionEvent{
			TxID:      tx.TxID,
			From:      tx.FromAccount,
			To:        tx.ToAccount,
			Amount:    tx.Amount,
			Type:      tx.TxType,
			Timestamp: tx.Timestamp.Format(time.RFC3339),
		}

		data, err := json.Marshal(event)
		if err != nil {
			continue
		}

		fmt.Fprintf(w, "event: transaction\ndata: %s\n\n", data)
		flusher.Flush()
	}

	// Check for errors
	select {
	case err := <-errCh:
		if err != nil {
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
			flusher.Flush()
		}
	default:
	}
}

// handleHealth handles GET /health
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Ping database to verify connectivity
	if err := s.db.Pool.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "unhealthy",
			"error":  err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorResponse{Error: message})
}
