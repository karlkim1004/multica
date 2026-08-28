package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/service"
)

const defaultWorkerPoolDispatchInterval = 5 * time.Minute

// runWorkerPoolDispatcher is the token-free queue watcher. It deliberately
// runs outside daemon/LLM sessions: it only reads candidate rows and invokes
// the normal task enqueue path when a lease-protected issue is eligible.
func runWorkerPoolDispatcher(ctx context.Context, pool *pgxpool.Pool, issues *service.IssueService) {
	interval := defaultWorkerPoolDispatchInterval
	if raw := os.Getenv("MULTICA_POOL_DISPATCH_INTERVAL"); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			interval = parsed
		} else {
			slog.Warn("pool dispatcher: invalid interval; using default", "value", raw, "default", interval)
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		dispatchWorkerPoolOnce(ctx, pool, issues)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func dispatchWorkerPoolOnce(ctx context.Context, pool *pgxpool.Pool, issues *service.IssueService) {
	rows, err := pool.Query(ctx, "SELECT id FROM workspace")
	if err != nil {
		slog.Warn("pool dispatcher: list workspaces failed", "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			slog.Warn("pool dispatcher: scan workspace failed", "error", err)
			return
		}
		issues.SweepStaleAssigned(ctx, id)
	}
	if err := rows.Err(); err != nil {
		slog.Warn("pool dispatcher: workspace iteration failed", "error", err)
	}
}
