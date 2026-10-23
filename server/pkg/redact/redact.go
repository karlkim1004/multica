// Package redact provides functions for detecting and masking secrets
// in agent output before it reaches the database or WebSocket broadcast.
package redact

import (
	"errors"
	"os"
	"os/user"
	"regexp"
	"strconv"
	"strings"
)

var ErrSecretStoreUnavailable = errors.New("secret store unavailable")

var chatPasswordPattern = regexp.MustCompile(`(?i)((?:비밀번호|비번|패스워드|암호|password|passwd|pw)\s*(?:(?:가|은|는|이|is)\s*|[:=]\s*|\s+))([A-Za-z0-9][^\s,;]*)`)
var chatPrivateKey = regexp.MustCompile(`(?s)-----BEGIN[A-Z\s]*PRIVATE KEY-----.*?-----END[A-Z\s]*PRIVATE KEY-----`)
var chatToken = regexp.MustCompile(`(?i)(?:AKIA[0-9A-Z]{16}|(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{20,255}|github_pat_[A-Za-z0-9_]{20,255}|sk-[A-Za-z0-9_-]{15,}|glpat-[A-Za-z0-9_-]{15,}|eyJ[A-Za-z0-9_-]{30,}|xox[bporase]-[A-Za-z0-9-]{10,}|xapp-[A-Za-z0-9-]{10,}|Bearer\s+[A-Za-z0-9._~+/=-]{20,})`)
var secretReference = regexp.MustCompile(`\[secret:[^\]\r\n]+\]`)
var secretLabelWithReference = regexp.MustCompile(`(?i)(?:비밀번호|비번|패스워드|암호|password|passwd|pw)\s*(?:(?:가|은|는|이|is)\s*|[:=]\s*|\s+)?\x00SECRETREF[^\x00]+\x00`)

func secretMarker(i int) string { return "\x00SECRETREF" + strconv.Itoa(i) + "\x00" }

// ReplaceSecrets encrypts detected credential values through store and leaves
// only opaque references in persisted chat text. Existing references are
// protected from being parsed as credential values again.
func ReplaceSecrets(input string, store func(string) (string, error)) (string, error) {
	refs := make([]string, 0)
	input = secretReference.ReplaceAllStringFunc(input, func(ref string) string {
		refs = append(refs, ref)
		return secretMarker(len(refs) - 1)
	})
	var storeErr error
	put := func(value string) string {
		if store == nil {
			storeErr = ErrSecretStoreUnavailable
			return "***"
		}
		ref, err := store(value)
		if err != nil {
			storeErr = err
			return "***"
		}
		refs = append(refs, "[secret:"+ref+"]")
		return secretMarker(len(refs) - 1)
	}
	if loc := chatPrivateKey.FindStringIndex(input); loc != nil {
		input = input[:loc[0]] + put(input[loc[0]:loc[1]]) + input[loc[1]:]
	}
	input = chatPasswordPattern.ReplaceAllStringFunc(input, func(match string) string {
		parts := chatPasswordPattern.FindStringSubmatch(match)
		return parts[1] + put(parts[2])
	})
	input = chatToken.ReplaceAllStringFunc(input, put)
	input = secretLabelWithReference.ReplaceAllStringFunc(input, func(labelRef string) string {
		refs = append(refs, labelRef)
		return secretMarker(len(refs) - 1)
	})
	input = secretReference.ReplaceAllStringFunc(input, func(ref string) string {
		refs = append(refs, ref)
		return secretMarker(len(refs) - 1)
	})
	input = Text(input)
	for i := len(refs) - 1; i >= 0; i-- {
		input = strings.ReplaceAll(input, secretMarker(i), refs[i])
	}
	return input, storeErr
}

// secretPattern pairs a compiled regex with its replacement text.
type secretPattern struct {
	re          *regexp.Regexp
	replacement string
}

// Patterns are checked in order; first match wins per position.
var patterns = []secretPattern{
	// AWS access key IDs (always start with AKIA)
	{regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), "[REDACTED AWS KEY]"},

	// AWS secret access keys (40 char base64-ish, preceded by a common separator)
	{regexp.MustCompile(`(?i)(?:aws_secret_access_key|secret_?access_?key)\s*[=:]\s*[A-Za-z0-9/+=]{40}`), "[REDACTED AWS SECRET]"},

	// PEM private keys (multi-line)
	{regexp.MustCompile(`(?s)-----BEGIN[A-Z\s]*PRIVATE KEY-----.*?-----END[A-Z\s]*PRIVATE KEY-----`), "[REDACTED PRIVATE KEY]"},

	// GitHub tokens (classic PAT, fine-grained, OAuth, etc.)
	{regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{36,255}\b`), "[REDACTED GITHUB TOKEN]"},

	// OpenAI / Anthropic API keys
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`), "[REDACTED API KEY]"},

	// Slack tokens
	{regexp.MustCompile(`\bxox[bporas]-[A-Za-z0-9\-]{10,}\b`), "[REDACTED SLACK TOKEN]"},

	// GitLab personal access tokens
	{regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`), "[REDACTED GITLAB TOKEN]"},

	// JWT tokens (three base64url segments)
	{regexp.MustCompile(`\bey[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`), "[REDACTED JWT]"},

	// Generic "Bearer <token>" in output
	{regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9\-._~+/]+=*\b`), "Bearer [REDACTED]"},

	// Connection strings with embedded passwords
	{regexp.MustCompile(`(?i)(?:postgres|mysql|mongodb|redis|amqp)(?:ql)?://[^:\s]+:[^@\s]+@`), "[REDACTED CONNECTION STRING]@"},

	// Generic key=value patterns for common secret env var names
	{regexp.MustCompile(`(?i)(?:API_KEY|API_SECRET|SECRET_KEY|SECRET|ACCESS_TOKEN|AUTH_TOKEN|PRIVATE_KEY|DATABASE_URL|DB_PASSWORD|DB_URL|REDIS_URL|PASSWORD|TOKEN)\s*[=:]\s*\S+`), "[REDACTED CREDENTIAL]"},
}

// InputMap returns a copy of m with all string values passed through Text.
// Non-string values are preserved as-is.
func InputMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = Text(s)
		} else {
			out[k] = v
		}
	}
	return out
}

// homeDir is resolved once at init for path redaction.
var homeDir string
var username string

func init() {
	homeDir, _ = os.UserHomeDir()
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
}

// Text scans the input string for known secret patterns and replaces
// matches with safe placeholders. It also masks the local user's home
// directory path to prevent leaking the username.
func Text(s string) string {
	for _, p := range patterns {
		s = p.re.ReplaceAllString(s, p.replacement)
	}

	// Redact home directory paths (e.g. /Users/john/ → /Users/****/).
	if homeDir != "" && username != "" {
		masked := strings.Replace(homeDir, username, "****", 1)
		s = strings.ReplaceAll(s, homeDir, masked)
	}

	return s
}
