package chatprivacy

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	"github.com/multica-ai/multica/server/pkg/redact"
)

// Vault encrypts detected chat credentials before they are persisted.
type Vault struct{ box *secretbox.Box }

func NewVault(box *secretbox.Box) *Vault { return &Vault{box: box} }

func (v *Vault) Sanitize(ctx context.Context, tx pgx.Tx, workspaceID, sessionID uuid.UUID, content string) (string, error) {
	return redact.ReplaceSecrets(content, func(value string) (string, error) {
		if v == nil || v.box == nil {
			return "", redact.ErrSecretStoreUnavailable
		}
		ref := uuid.New()
		ciphertext, err := v.box.Seal([]byte(value))
		if err != nil {
			return "", errors.New("encrypt chat secret")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO chat_secret (secret_ref, workspace_id, chat_session_id, ciphertext) VALUES ($1, $2, $3, $4)`, ref, workspaceID, sessionID, ciphertext); err != nil {
			return "", fmt.Errorf("store encrypted chat secret: %w", err)
		}
		return ref.String(), nil
	})
}
