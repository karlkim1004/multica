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

// workerPoolDispatchInterval configures how often the existing runtime
// sweeper invokes the token-free queue scan. It does not create another
// goroutine or ticker.
func workerPoolDispatchInterval() time.Duration {
	interval := defaultWorkerPoolDispatchInterval
	if raw := os.Getenv("MULTICA_POOL_DISPATCH_INTERVAL"); raw != "" {
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			interval = parsed
		} else {
			slog.Warn("pool dispatcher: invalid interval; using default", "value", raw, "default", interval)
		}
	}
	return interval
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
