package daemon

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"

	"github.com/google/uuid"
)

func chatSecretKeychainInput(reference, value string) (string, error) {
	if _, err := uuid.Parse(reference); err != nil {
		return "", errors.New("invalid chat secret reference")
	}
	if value == "" {
		return "", errors.New("empty chat secret")
	}
	return fmt.Sprintf("add-generic-password -U -a %s -s com.multica.chat -X %s\n", reference, hex.EncodeToString([]byte(value))), nil
}

func syncChatSecretsToKeychain(ctx context.Context, secrets []ChatSecret) error {
	if len(secrets) == 0 {
		return nil
	}
	if runtime.GOOS != "darwin" {
		return errors.New("chat secret keychain sync requires macOS")
	}
	for _, secret := range secrets {
		input, err := chatSecretKeychainInput(secret.Reference, secret.Value)
		if err != nil {
			return errors.New("invalid chat secret data")
		}
		cmd := exec.CommandContext(ctx, "security", "-i")
		cmd.Stdin = strings.NewReader(input)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if err := cmd.Run(); err != nil {
			return errors.New("chat secret keychain sync failed")
		}
	}
	return nil
}
