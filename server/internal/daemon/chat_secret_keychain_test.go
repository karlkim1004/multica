package daemon

import (
	"bytes"
	"context"
	"encoding/hex"
	"strings"
	"testing"
)

func TestChatSecretKeychainCommandKeepsValueOutOfArgv(t *testing.T) {
	const ref = "123e4567-e89b-12d3-a456-426614174000"
	const value = "DummyProbe123"
	cmd, input, err := newChatSecretKeychainCommand(context.Background(), ref, value)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(input)
	if strings.Join(cmd.Args, " ") != "security -i" {
		t.Fatalf("secret keychain command args contain more than the interactive mode: %#v", cmd.Args)
	}
	if bytes.Contains(input, []byte(value)) {
		t.Fatal("raw secret was written to security command input")
	}
	const prefix = "add-generic-password -U -a " + ref + " -s MulticaChat -X "
	line := strings.TrimSpace(strings.TrimPrefix(string(input), prefix))
	decoded, err := hex.DecodeString(line)
	if err != nil || string(decoded) != value {
		t.Fatalf("security stdin encoding did not round-trip (match=%t err=%v)", string(decoded) == value, err)
	}
	clear(decoded)
}

func TestChatSecretKeychainCommandRejectsInvalidReference(t *testing.T) {
	if _, _, err := newChatSecretKeychainCommand(context.Background(), "bad\nquit", "value"); err == nil {
		t.Fatal("invalid reference could inject an interactive security command")
	}
}
