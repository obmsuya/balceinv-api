package database

import (
	"context"
	"database/sql"
	"sync/atomic"
)

type CountingQuerier struct {
	Inner   Querier
	Counter *atomic.Int64
}

func (querier *CountingQuerier) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	querier.Counter.Add(1)
	return querier.Inner.ExecContext(ctx, query, args...)
}

func (querier *CountingQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	querier.Counter.Add(1)
	return querier.Inner.QueryContext(ctx, query, args...)
}

func (querier *CountingQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	querier.Counter.Add(1)
	return querier.Inner.QueryRowContext(ctx, query, args...)
}
