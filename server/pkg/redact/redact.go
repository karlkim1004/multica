// Package redact provides functions for detecting and masking secrets
// in agent output before it reaches the database or WebSocket broadcast.
package redact

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var ErrSecretStoreUnavailable = errors.New("secret store unavailable")

// secretPattern pairs a compiled regex with its replacement text.
type secretPattern struct {
	re          *regexp.Regexp
	replacement string
}

// Patterns are checked in order; first match wins per position.
var patterns = []secretPattern{
	// Human-entered passwords are commonly labeled in Korean or English.
	{regexp.MustCompile(`(?i)(?:비밀번호|비번|password|passwd|pw)\s*(?:가|은|는|[:=])\s*[^\s,;]+`), "[REDACTED CREDENTIAL]"},

	// AWS access key IDs (always start with AKIA)
	{regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), "[REDACTED AWS KEY]"},

	// AWS secret access keys (40 char base64-ish, preceded by a common separator)
	{regexp.MustCompile(`(?i)(?:aws_secret_access_key|secret_?access_?key)\s*[=:]\s*[A-Za-z0-9/+=]{40}`), "[REDACTED AWS SECRET]"},

	// PEM private keys (multi-line)
	{regexp.MustCompile(`(?s)-----BEGIN[A-Z\s]*PRIVATE KEY-----.*?-----END[A-Z\s]*PRIVATE KEY-----`), "[REDACTED PRIVATE KEY]"},

	// GitHub tokens (classic PAT, OAuth, user-to-server, server-to-server, refresh)
	{regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{36,255}\b`), "[REDACTED GITHUB TOKEN]"},

	// GitHub fine-grained personal access tokens use the github_pat_ prefix,
	// which the classic ghp_/gho_/... pattern above does not cover. Without
	// this line a fine-grained PAT emitted in agent output leaks unredacted
	// to the database and WebSocket broadcast.
	{regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,255}\b`), "[REDACTED GITHUB TOKEN]"},

	// OpenAI / Anthropic API keys
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`), "[REDACTED API KEY]"},

	// Slack bot/user/legacy tokens. The char class includes 'e' so the
	// newer xoxe- config/refresh tokens are covered alongside xoxb/p/o/r/a/s.
	{regexp.MustCompile(`\bxox[bporase]-[A-Za-z0-9\-]{10,}\b`), "[REDACTED SLACK TOKEN]"},

	// Slack app-level tokens use the xapp- prefix, which the xox*- rule above
	// does not match. Without this an app-level token echoed in agent output
	// leaks unredacted to the DB / WebSocket broadcast.
	{regexp.MustCompile(`\bxapp-[A-Za-z0-9-]{10,}\b`), "[REDACTED SLACK TOKEN]"},

	// GitLab personal access tokens
	{regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`), "[REDACTED GITLAB TOKEN]"},

	// Google API keys always start with the AIza prefix and are 39 chars total
	// (AIza + 35). Capture and restore the trailing delimiter so keys ending in
	// a non-word character such as '-' are still redacted.
	{regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}([^0-9A-Za-z_-]|$)`), "[REDACTED GOOGLE API KEY]$1"},

	// Stripe secret / restricted live keys (sk_live_ / rk_live_). The sk-
	// rule above only matches the hyphen form used by OpenAI/Anthropic; Stripe
	// uses an underscore, so live keys are not covered without this. Publishable
	// keys (pk_live_) are intentionally excluded — they are not secret.
	{regexp.MustCompile(`\b(?:sk|rk)_live_[0-9A-Za-z]{16,}\b`), "[REDACTED STRIPE KEY]"},

	// JWT tokens (three base64url segments)
	{regexp.MustCompile(`\bey[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`), "[REDACTED JWT]"},
	// JWT-like tokens are also sometimes pasted without separators.
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{30,}\b`), "[REDACTED JWT]"},

	// Generic "Bearer <token>" in output
	{regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9\-._~+/]+=*\b`), "Bearer [REDACTED]"},

	// Connection strings with embedded passwords
	{regexp.MustCompile(`(?i)(?:postgres|mysql|mongodb|redis|amqp)(?:ql)?://[^:\s]+:[^@\s]+@`), "[REDACTED CONNECTION STRING]@"},

	// Generic key=value patterns for common secret env var names
	{regexp.MustCompile(`(?i)(?:API_KEY|API_SECRET|SECRET_KEY|SECRET|ACCESS_TOKEN|AUTH_TOKEN|PRIVATE_KEY|DATABASE_URL|DB_PASSWORD|DB_URL|REDIS_URL|PASSWORD|TOKEN)\s*[=:]\s*\S+`), "[REDACTED CREDENTIAL]"},
}

var (
	chatPasswordPattern  = regexp.MustCompile(`(?i)((?:비밀번호|비번|패스워드|암호|password|passwd|pw)(?:\s*(?:가|은|는|이|를|을|is))?\s*(?:[:=]\s*|\s+))([^\s,;]+)`)
	chatLabeledSecretRef = regexp.MustCompile(`(?i)(?:비밀번호|비번|패스워드|암호|password|passwd|pw)(?:\s*(?:가|은|는|이|를|을|is))?\s*(?:[:=]\s*|\s+)\[secret:[^\]]+\]`)
	chatPrivateKey       = regexp.MustCompile(`(?s)-----BEGIN[A-Z\s]*PRIVATE KEY-----.*?-----END[A-Z\s]*PRIVATE KEY-----`)
	chatToken            = regexp.MustCompile(`(?i)(?:AKIA[0-9A-Z]{16}|(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{20,255}|github_pat_[A-Za-z0-9_]{20,255}|sk-[A-Za-z0-9_-]{15,}|glpat-[A-Za-z0-9_-]{15,}|eyJ[A-Za-z0-9_-]{30,}|xox[bporase]-[A-Za-z0-9-]{10,}|xapp-[A-Za-z0-9-]{10,}|Bearer\s+[A-Za-z0-9._~+/=-]{20,})`)
	secretRefPattern     = regexp.MustCompile(`\[secret:[^\]]+\]`)
)

// Korean sentence endings sometimes attach directly to a pasted password.
var koreanPasswordTails = []string{"이라고요", "이라네요", "입니다", "이에요", "예요", "에요", "이야", "라고요", "라고", "이네", "네요", "임", "야", "요", "다"}

func splitPasswordValue(raw string) (value, suffix string) {
	end := len(raw)
	for end > 0 {
		r, size := utf8.DecodeLastRuneInString(raw[:end])
		if strings.ContainsRune(".!?。！？)]}>", r) {
			end -= size
			continue
		}
		break
	}
	punctuation := raw[end:]
	core := raw[:end]
	for _, tail := range koreanPasswordTails {
		if strings.HasSuffix(core, tail) {
			candidate := strings.TrimSuffix(core, tail)
			if hasCredentialRune(candidate) {
				return candidate, tail + punctuation
			}
		}
	}
	return core, punctuation
}

func hasCredentialRune(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
			return true
		}
	}
	return false
}

// ReplaceSecrets stores detected values through store and replaces them with
// [secret:<ref>]. When storage fails, values are still masked and an error
// prevents callers from persisting a broken reference or original input.
func ReplaceSecrets(input string, store func(string) (string, error)) (string, error) {
	var storeErr error
	refs := make([]string, 0)
	protect := func(value string) string {
		token := fmt.Sprintf("ZZREF%dZZ", len(refs))
		refs = append(refs, value)
		return token
	}
	// Preserve an existing reference together with its label before scanning
	// credentials. Otherwise the password matcher treats [secret:...] as a new
	// value and stores it again, while Text() can also redact the label.
	input = chatLabeledSecretRef.ReplaceAllStringFunc(input, protect)
	input = secretRefPattern.ReplaceAllStringFunc(input, protect)
	storeValue := func(value string) string {
		if store == nil {
			storeErr = ErrSecretStoreUnavailable
			return "***"
		}
		ref, err := store(value)
		if err != nil {
			storeErr = err
			return "***"
		}
		return "[secret:" + ref + "]"
	}
	if loc := chatPrivateKey.FindStringIndex(input); loc != nil {
		input = input[:loc[0]] + storeValue(input[loc[0]:loc[1]]) + input[loc[1]:]
	}
	input = chatPasswordPattern.ReplaceAllStringFunc(input, func(match string) string {
		parts := chatPasswordPattern.FindStringSubmatch(match)
		value, suffix := splitPasswordValue(parts[2])
		// A label followed by ordinary Korean help text is not a credential.
		// Requiring an ASCII letter or digit avoids storing phrases like
		// "비밀번호 변경 방법" as a secret.
		if !hasCredentialRune(value) {
			return match
		}
		return parts[1] + storeValue(value) + suffix
	})
	input = chatToken.ReplaceAllStringFunc(input, storeValue)
	// References created above must also bypass Text(), especially when the
	// generic PASSWORD/SECRET rules see their surrounding human label.
	input = chatLabeledSecretRef.ReplaceAllStringFunc(input, protect)
	input = secretRefPattern.ReplaceAllStringFunc(input, protect)
	input = Text(input)
	for i, ref := range refs {
		input = strings.ReplaceAll(input, fmt.Sprintf("ZZREF%dZZ", i), ref)
	}
	return input, storeErr
}

// maxRedactDepth bounds the walk in redactValue. Tool inputs are decoded from
// daemon-supplied JSON, so nesting depth is attacker-influenced; without a
// bound a pathologically nested payload would recurse until the stack blows and
// take the process down. Real tool inputs nest a handful of levels at most, so
// this only ever trips on abuse.
const maxRedactDepth = 32

// depthLimitPlaceholder replaces anything below maxRedactDepth. Returning the
// raw value there would hand back an unscrubbed string, which is exactly what
// this package exists to prevent, so the fail-safe direction is to drop it.
const depthLimitPlaceholder = "[REDACTED DEPTH LIMIT]"

// InputMap returns a copy of m with every string value passed through Text,
// including strings nested inside maps and slices.
//
// The nested walk is load-bearing, not defensive tidying: providers record
// structured tool inputs, and Codex records a file edit as
// changes[]{path, diff, content}. A top-level-only pass leaves a credential
// inside a patch body — or the full contents of a deleted .env — untouched on
// its way to the database and the WebSocket broadcast.
func InputMap(m map[string]any) map[string]any {
	return redactMap(m, 0)
}

func redactMap(m map[string]any, depth int) map[string]any {
	if m == nil {
		return nil
	}
	if depth >= maxRedactDepth {
		return map[string]any{"_": depthLimitPlaceholder}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = redactValue(v, depth+1)
	}
	return out
}

// redactValue scrubs a single decoded JSON value, recursing through the
// composite shapes json.Unmarshal produces plus []string, which providers use
// for argv-style inputs.
//
// Composites are copied rather than scrubbed in place: the caller still holds
// the original map and keeps using it after redaction (the daemon handler logs
// and re-reads it), so mutating through the shared reference would be a
// surprise at a distance.
func redactValue(v any, depth int) any {
	if depth >= maxRedactDepth {
		return depthLimitPlaceholder
	}
	switch t := v.(type) {
	case string:
		return Text(t)
	case map[string]any:
		return redactMap(t, depth)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = redactValue(e, depth+1)
		}
		return out
	case []string:
		out := make([]string, len(t))
		for i, e := range t {
			out[i] = Text(e)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(t))
		for k, e := range t {
			out[k] = Text(e)
		}
		return out
	default:
		return v
	}
}

// Text scans the input string for known secret patterns and replaces
// matches with safe placeholders.
//
// It deliberately does NOT mask the local home directory. That masking used to
// live here, but it was never a boundary: anyone who can read a transcript
// already sees repository paths, file contents, commands and diffs, so hiding
// one path segment protected nothing while making paths unusable for copy-paste
// and debugging. It was also incoherent about whose home directory it hid —
// Content/Output are redacted in the server's ingest handler, so a hosted
// deployment matched them against the *server's* home, never the machine
// running the agent. Transcript visibility is an authorization concern and is
// handled at that layer, not by string replacement here.
func Text(s string) string {
	for _, p := range patterns {
		s = p.re.ReplaceAllString(s, p.replacement)
	}
	return s
}
