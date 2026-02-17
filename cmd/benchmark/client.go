package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kaldun-tech/grpc-rest-benchmark/pkg/protos"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// BenchmarkClient abstracts gRPC and REST for uniform benchmarking.
type BenchmarkClient interface {
	GetBalance(ctx context.Context, accountID string) error
	StreamTransactions(ctx context.Context, rate int) (<-chan StreamEvent, <-chan error)
	Close() error
}

// StreamEvent represents a received streaming event.
type StreamEvent struct {
	ReceivedAt time.Time
}

// gRPCClient implements BenchmarkClient using gRPC.
type gRPCClient struct {
	conn      *grpc.ClientConn
	balance   protos.BalanceServiceClient
	txService protos.TransactionServiceClient
}

// NewGRPCClient creates a new gRPC benchmark client.
func NewGRPCClient(addr string) (BenchmarkClient, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to gRPC server: %w", err)
	}

	return &gRPCClient{
		conn:      conn,
		balance:   protos.NewBalanceServiceClient(conn),
		txService: protos.NewTransactionServiceClient(conn),
	}, nil
}

func (c *gRPCClient) GetBalance(ctx context.Context, accountID string) error {
	_, err := c.balance.GetBalance(ctx, &protos.BalanceRequest{AccountId: accountID})
	return err
}

func (c *gRPCClient) StreamTransactions(ctx context.Context, rate int) (<-chan StreamEvent, <-chan error) {
	eventCh := make(chan StreamEvent, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(errCh)

		stream, err := c.txService.StreamTransactions(ctx, &protos.StreamRequest{
			RateLimit: int32(rate),
		})
		if err != nil {
			errCh <- fmt.Errorf("failed to start stream: %w", err)
			return
		}

		for {
			_, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				errCh <- fmt.Errorf("stream received error: %w", err)
				return
			}

			select {
			case eventCh <- StreamEvent{ReceivedAt: time.Now()}:
			case <-ctx.Done():
				return
			}
		}
	}()

	return eventCh, errCh
}

func (c *gRPCClient) Close() error {
	return c.conn.Close()
}

// httpClient implements BenchmarkClient using HTTP/REST.
type httpClient struct {
	client  *http.Client
	baseURL string
}

// NewHTTPClient creates a new HTTP benchmark client.
func NewHTTPClient(baseURL string) (BenchmarkClient, error) {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}

	return &httpClient{
		client: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
		baseURL: strings.TrimSuffix(baseURL, "/"),
	}, nil
}

func (c *httpClient) GetBalance(ctx context.Context, accountID string) error {
	url := fmt.Sprintf("%s/api/v1/accounts/%s/balance", c.baseURL, accountID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Drain the body to allow connection reuse
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	return nil
}

func (c *httpClient) StreamTransactions(ctx context.Context, rate int) (<-chan StreamEvent, <-chan error) {
	eventCh := make(chan StreamEvent, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(errCh)

		url := fmt.Sprintf("%s/api/v1/transactions/stream", c.baseURL)
		if rate > 0 {
			url = fmt.Sprintf("%s?rate=%d", url, rate)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			errCh <- fmt.Errorf("failed to create request: %w", err)
			return
		}
		req.Header.Set("Accept", "text/event-stream")

		resp, err := c.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			errCh <- fmt.Errorf("request failed: %w", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			errCh <- fmt.Errorf("unexpected status: %d", resp.StatusCode)
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()

			// SSE format: "data: {...}"
			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				var event map[string]interface{}
				if err := json.Unmarshal([]byte(data), &event); err != nil {
					continue
				}

				select {
				case eventCh <- StreamEvent{ReceivedAt: time.Now()}:
				case <-ctx.Done():
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			if ctx.Err() != nil {
				return
			}
			errCh <- fmt.Errorf("scanner error: %w", err)
		}
	}()

	return eventCh, errCh
}

func (c *httpClient) Close() error {
	c.client.CloseIdleConnections()
	return nil
}
