## Go Idioms

1. It's fine in Go to have a long main method. This is idiomatic to keep this as a routine for orchestration. Comment blocks as section headers are acceptable.
2. Defer blocks are executed in LIFO order at the end of the enclosing function.
3. Always check `rows.Err()` after iterating with `rows.Next()` - the loop exits on error but doesn't surface it.

## Database Patterns (pgx)

4. Rows objects returned by `db.Pool.Query` can be iterated with `for rows.Next()`. Use `defer rows.Close()` immediately after the Query call.
5. For batch inserts, wrap in a transaction: `tx.Begin()`, loop with `tx.Exec()`, then `tx.Commit()`. Use `defer tx.Rollback(ctx)` which is a no-op after commit.
6. `ORDER BY RANDOM() LIMIT 1` is simple but slow on large tables. Fine for 10K rows, doesn't scale to millions.

## Streaming Patterns

7. Return a pair of channels `(<-chan T, <-chan error)` for async streaming. See `pkg/db/transactions.go`. Close both channels in the goroutine with defer.
8. For SSE (Server-Sent Events): set `WriteTimeout: 0`, use `http.Flusher`, format as `event: name\ndata: json\n\n`.

## Protocol Buffers

9. Generated `.pb.go` files may contain deprecated methods for backwards compatibility. Don't edit these files.
10. Schemas align across layers: internal structs (`pkg/db/`), database columns, and protobuf messages. Keep field names consistent where possible.
