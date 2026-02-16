package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kaldun-tech/grpc-rest-benchmark/pkg/db"
	"github.com/kaldun-tech/grpc-rest-benchmark/pkg/protos"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

var (
	port     = flag.Int("port", 50051, "gRPC server port")
	dbHost   = flag.String("db-host", "localhost", "PostgreSQL host")
	dbPort   = flag.Int("db-port", 5432, "PostgreSQL port")
	dbUser   = flag.String("db-user", "benchmark", "PostgreSQL user")
	dbPass   = flag.String("db-pass", "benchmark_pass", "PostgreSQL password")
	dbName   = flag.String("db-name", "grpc_benchmark", "PostgreSQL database")
)

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

	// Create gRPC server
	server := grpc.NewServer()

	// Register services
	balanceService := NewBalanceService(database)
	protos.RegisterBalanceServiceServer(server, balanceService)

	transactionService := NewTransactionService(database)
	protos.RegisterTransactionServiceServer(server, transactionService)

	// Register health service
	healthServer := health.NewServer()
	grpc_health_v1.RegisterHealthServer(server, healthServer)
	healthServer.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)

	// Enable reflection for debugging with grpcurl
	reflection.Register(server)

	// Start listening
	addr := fmt.Sprintf(":%d", *port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", addr, err)
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down gRPC server...")
		server.GracefulStop()
	}()

	log.Printf("gRPC server listening on %s", addr)
	if err := server.Serve(listener); err != nil {
		log.Fatalf("Failed to serve: %v", err)
	}
}

// BalanceService implements the BalanceService gRPC service.
type BalanceService struct {
	protos.UnimplementedBalanceServiceServer
	db *db.DB
}

// NewBalanceService creates a new BalanceService.
func NewBalanceService(database *db.DB) *BalanceService {
	return &BalanceService{db: database}
}

// GetBalance returns the balance for a single account.
func (s *BalanceService) GetBalance(ctx context.Context, req *protos.BalanceRequest) (*protos.BalanceResponse, error) {
	account, err := s.db.GetBalance(ctx, req.AccountId)
	if err != nil {
		return nil, err
	}

	return &protos.BalanceResponse{
		AccountId:     account.AccountID,
		BalanceTinybar: account.Balance,
		Timestamp:     account.UpdatedAt.Format(time.RFC3339),
	}, nil
}

// GetBalances returns balances for multiple accounts.
func (s *BalanceService) GetBalances(ctx context.Context, req *protos.BatchBalanceRequest) (*protos.BatchBalanceResponse, error) {
	accounts, err := s.db.GetBalances(ctx, req.AccountIds)
	if err != nil {
		return nil, err
	}

	balances := make([]*protos.BalanceResponse, len(accounts))
	for i, acc := range accounts {
		balances[i] = &protos.BalanceResponse{
			AccountId:     acc.AccountID,
			BalanceTinybar: acc.Balance,
			Timestamp:     acc.UpdatedAt.Format(time.RFC3339),
		}
	}

	return &protos.BatchBalanceResponse{Balances: balances}, nil
}

// TransactionService implements the TransactionService gRPC service.
type TransactionService struct {
	protos.UnimplementedTransactionServiceServer
	db *db.DB
}

// NewTransactionService creates a new TransactionService.
func NewTransactionService(database *db.DB) *TransactionService {
	return &TransactionService{db: database}
}

// StreamTransactions streams transactions to the client.
func (s *TransactionService) StreamTransactions(req *protos.StreamRequest, stream protos.TransactionService_StreamTransactionsServer) error {
	ctx := stream.Context()

	// Parse since timestamp
	var since time.Time
	if req.SinceTimestamp != "" {
		var err error
		since, err = time.Parse(time.RFC3339, req.SinceTimestamp)
		if err != nil {
			since = time.Time{} // Default to beginning
		}
	}

	opts := db.StreamTransactionsOptions{
		Since:         since,
		FilterAccount: req.FilterAccount,
	}

	txCh, errCh := s.db.StreamTransactions(ctx, opts)

	// Rate limiting
	var ticker *time.Ticker
	if req.RateLimit > 0 {
		ticker = time.NewTicker(time.Second / time.Duration(req.RateLimit))
		defer ticker.Stop()
	}

	for tx := range txCh {
		// Apply rate limiting if configured
		if ticker != nil {
			select {
			case <-ticker.C:
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		protoTx := &protos.Transaction{
			TxId:          tx.TxID,
			FromAccount:   tx.FromAccount,
			ToAccount:     tx.ToAccount,
			AmountTinybar: tx.Amount,
			TxType:        tx.TxType,
			Timestamp:     tx.Timestamp.Format(time.RFC3339),
		}

		if err := stream.Send(protoTx); err != nil {
			return err
		}
	}

	// Check for errors from the stream
	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	default:
	}

	return nil
}
