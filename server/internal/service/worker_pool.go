package service

import (
	"context"
	"errors"
	"path"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const workerPoolLeaseSeconds int32 = 300

var ErrWorkerPoolLockHeld = errors.New("worker pool lock is held")

// WorkerPoolService owns portable persona cloning and expiring work leases.
//
// Persona copy policy:
//   - copied: name, description, avatar, instructions, model, thinking level,
//     visibility, owner and concurrency limit.
//   - never copied: runtime_config, custom_env, custom_args, mcp_config,
//     task/session/work-dir state and runtime identity.
//
// The latter group is runtime- or credential-bound and must be provisioned by
// the destination runtime, not duplicated by scheduling.
type WorkerPoolService struct{ Queries *db.Queries }

func NewWorkerPoolService(q *db.Queries) *WorkerPoolService { return &WorkerPoolService{Queries: q} }

func (s *WorkerPoolService) ClonePersonaToRuntime(ctx context.Context, sourceID, workspaceID, targetRuntimeID pgtype.UUID) (db.Agent, error) {
	return s.Queries.ClonePersonaToRuntime(ctx, db.ClonePersonaToRuntimeParams{
		ID:          sourceID,
		WorkspaceID: workspaceID,
		ID_2:        targetRuntimeID,
	})
}

func (s *WorkerPoolService) AcquireIssueLock(ctx context.Context, workspaceID, issueID, holderID pgtype.UUID) error {
	return s.acquire(ctx, workspaceID, "issue", issueID.String(), holderID)
}

func (s *WorkerPoolService) AcquireFileLock(ctx context.Context, workspaceID pgtype.UUID, filePath string, holderID pgtype.UUID) error {
	clean := path.Clean("/" + strings.TrimSpace(filePath))
	if clean == "/" || strings.HasPrefix(clean, "/../") {
		return errors.New("worker pool file lock requires a project-relative path")
	}
	return s.acquire(ctx, workspaceID, "file", clean, holderID)
}

func (s *WorkerPoolService) acquire(ctx context.Context, workspaceID pgtype.UUID, resourceType, resourceKey string, holderID pgtype.UUID) error {
	_, err := s.Queries.AcquireWorkerPoolLock(ctx, db.AcquireWorkerPoolLockParams{
		WorkspaceID: workspaceID, ResourceType: resourceType, ResourceKey: resourceKey,
		HolderID: holderID, LeaseSeconds: float64(workerPoolLeaseSeconds),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrWorkerPoolLockHeld
	}
	return err
}

func (s *WorkerPoolService) ReleaseIssueLock(ctx context.Context, workspaceID, issueID, holderID pgtype.UUID) error {
	_, err := s.Queries.ReleaseWorkerPoolLock(ctx, db.ReleaseWorkerPoolLockParams{WorkspaceID: workspaceID, ResourceType: "issue", ResourceKey: issueID.String(), HolderID: holderID})
	return err
}

func (s *WorkerPoolService) ReleaseFileLock(ctx context.Context, workspaceID pgtype.UUID, filePath string, holderID pgtype.UUID) error {
	clean := path.Clean("/" + strings.TrimSpace(filePath))
	_, err := s.Queries.ReleaseWorkerPoolLock(ctx, db.ReleaseWorkerPoolLockParams{WorkspaceID: workspaceID, ResourceType: "file", ResourceKey: clean, HolderID: holderID})
	return err
}
