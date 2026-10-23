package chatprivacy

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	"github.com/multica-ai/multica/server/pkg/redact"
)

// Vault encrypts secret values before writing them into chat_secret.
type Vault struct{ box *secretbox.Box }

func NewVault(box *secretbox.Box) *Vault { return &Vault{box: box} }

// Sanitize stores every detected value in the caller's transaction and emits
// only opaque references into the chat transcript.
func (v *Vault) Sanitize(ctx context.Context, tx pgx.Tx, workspaceID, sessionID string, content string) (string, error) {
	if v == nil || v.box == nil {
		return redact.ReplaceSecrets(content, nil)
	}
	return redact.ReplaceSecrets(content, func(value string) (string, error) {
		ref := uuid.New()
		ciphertext, err := v.box.Seal([]byte(value))
		if err != nil {
			return "", fmt.Errorf("encrypt chat secret")
		}
		if _, err := tx.Exec(ctx, `INSERT INTO chat_secret (secret_ref, workspace_id, chat_session_id, ciphertext) VALUES ($1, $2, $3, $4)`, ref, workspaceID, sessionID, ciphertext); err != nil {
			return "", fmt.Errorf("store encrypted chat secret")
		}
		return ref.String(), nil
	})
}
