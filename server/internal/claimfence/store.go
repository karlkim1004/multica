// Package claimfence contains an experimental transactional admission primitive.
// It is not wired into the daemon or server and does not constitute a drain.
package claimfence

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrFenced = errors.New("runtime admission is fenced")
var ErrFenceOwned = errors.New("runtime admission fence is owned")

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// lockRuntime serializes fence acquisition and claims, including initial row creation.
func lockRuntime(ctx context.Context, tx pgx.Tx, runtimeID uuid.UUID) (*uuid.UUID, int64, error) {
	if runtimeID == uuid.Nil {
		return nil, 0, errors.New("runtime ID is required")
	}
	if _, err := tx.Exec(ctx, `INSERT INTO claim_fence(runtime_id) VALUES ($1) ON CONFLICT DO NOTHING`, runtimeID); err != nil {
		return nil, 0, err
	}
	var owner *uuid.UUID
	var epoch int64
	err := tx.QueryRow(ctx, `SELECT owner_id, epoch FROM claim_fence WHERE runtime_id=$1 FOR UPDATE`, runtimeID).Scan(&owner, &epoch)
	return owner, epoch, err
}

// Acquire closes admission and returns the durable fence epoch. Repeating the
// same owner is idempotent; another owner cannot take over an active fence.
func (s *Store) Acquire(ctx context.Context, runtimeID, ownerID uuid.UUID) (int64, error) {
	if ownerID == uuid.Nil {
		return 0, errors.New("owner ID is required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(context.Background())
	owner, epoch, err := lockRuntime(ctx, tx, runtimeID)
	if err != nil {
		return 0, err
	}
	if owner != nil {
		if *owner != ownerID {
			return 0, ErrFenceOwned
		}
	} else {
		err = tx.QueryRow(ctx, `UPDATE claim_fence SET owner_id=$2, epoch=epoch+1 WHERE runtime_id=$1 RETURNING epoch`, runtimeID, ownerID).Scan(&epoch)
		if err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return epoch, nil
}

// Claim runs the assignment callback and stores its receipt in one transaction.
// The callback must only perform transactional database operations; it must not
// send events or start processes. A nil task is a durable empty receipt.
func (s *Store) Claim(ctx context.Context, runtimeID, operationID uuid.UUID, assign func(context.Context, pgx.Tx) (*uuid.UUID, error)) (*uuid.UUID, error) {
	if operationID == uuid.Nil || assign == nil {
		return nil, errors.New("operation ID and assignment are required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.Background())
	owner, _, err := lockRuntime(ctx, tx, runtimeID)
	if err != nil {
		return nil, err
	}
	var taskID *uuid.UUID
	err = tx.QueryRow(ctx, `SELECT task_id FROM claim_receipt WHERE runtime_id=$1 AND operation_id=$2`, runtimeID, operationID).Scan(&taskID)
	if err == nil {
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		return taskID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if owner != nil {
		return nil, ErrFenced
	}
	taskID, err = assign(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("claim assignment: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO claim_receipt(runtime_id, operation_id, task_id) VALUES ($1, $2, $3)`, runtimeID, operationID, taskID); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return taskID, nil
}
