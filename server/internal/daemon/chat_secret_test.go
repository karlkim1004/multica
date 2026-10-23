package daemon

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestChatSecretKeychainInputDoesNotExposeValueAsArgument(t *testing.T) {
	value := "SensitiveAbc1234"
	input, err := chatSecretKeychainInput("123e4567-e89b-12d3-a456-426614174000", value)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(input, value) {
		t.Fatal("stdin command contains raw value")
	}
	if !strings.Contains(input, hex.EncodeToString([]byte(value))) {
		t.Fatal("expected hex-encoded secret on stdin")
	}
	if _, err := chatSecretKeychainInput("not-a-uuid", value); err == nil {
		t.Fatal("invalid reference accepted")
	}
}
