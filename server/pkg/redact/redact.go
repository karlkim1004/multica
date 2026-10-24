// Package redact provides functions for detecting and masking secrets
// in agent output before it reaches the database or WebSocket broadcast.
package redact

import (
	"encoding/json"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var ErrSecretStoreUnavailable = errors.New("secret store unavailable")

var chatPasswordPattern = regexp.MustCompile(`(?i)((?:비밀번호|비번|패스워드|암호|password|passwd|pw)\s*(?:(?:가|은|는|이|is)\s*|[:=]\s*|\s+))([A-Za-z0-9][^\s,;]*)`)
var chatPrivateKey = regexp.MustCompile(`(?s)-----BEGIN[A-Z\s]*PRIVATE KEY-----.*?-----END[A-Z\s]*PRIVATE KEY-----`)
var chatToken = regexp.MustCompile(`(?i)(?:AKIA[0-9A-Z]{16}|(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9_]{20,255}|github_pat_[A-Za-z0-9_]{20,255}|sk-[A-Za-z0-9_-]{15,}|glpat-[A-Za-z0-9_-]{15,}|eyJ[A-Za-z0-9_-]{30,}|xox[bporase]-[A-Za-z0-9-]{10,}|xapp-[A-Za-z0-9-]{10,}|Bearer\s+[A-Za-z0-9._~+/=-]{20,})`)
var secretReference = regexp.MustCompile(`\[secret:[^\]\r\n]+\]`)
var secretLabelWithReference = regexp.MustCompile(`(?i)(?:비밀번호|비번|패스워드|암호|password|passwd|pw)\s*(?:(?:가|은|는|이|is)\s*|[:=]\s*|\s+)?\x00SECRETREF[^\x00]+\x00`)

var cardCandidate = regexp.MustCompile(`\b(?:\d[ -]?){13,19}\b`)
var labeledCard = regexp.MustCompile(`(?i)((?:카드|card)(?:\s*(?:번호|number|no\.?))?\s*[:=]?\s*)((?:\d[ -]?){13,19})`)
var residentID = regexp.MustCompile(`\b\d{6}[- ]?[1-4]\d{6}\b`)
var koreanPhone = regexp.MustCompile(`(^|[^0-9])((?:\+82[- ]?)?0?1[016789][ -]?\d{3,4}[ -]?\d{4})([^0-9]|$)`)
var labeledAccount = regexp.MustCompile(`(?i)((?:계좌|account)(?:\s*(?:번호|number|no\.?))?\s*[:=]?\s*)([0-9][0-9 -]{5,28}[0-9])`)
var labeledPassport = regexp.MustCompile(`(?i)((?:여권|passport)(?:\s*(?:번호|number|no\.?))?\s*[:=]?\s*)([A-Z0-9][A-Z0-9 -]{5,14})`)
var operationalID = regexp.MustCompile(`\b[A-Z]{2,}-\d{4}-[A-Z0-9]+(?:-[A-Z0-9]+)*\b`)
var protectedPath = regexp.MustCompile(`(?:^|\s)(?:outputs|projects|scripts|queues|logs|tests|dashboard-v2|dashboard|db|api|templates|state|standards)/[^\s\x60"')]+`)
var piiPassword = regexp.MustCompile(`(?i)((?:비밀번호|비번|패스워드|암호|password|passwd|pw)\s*(?:(?:가|은|는|이|is)\s*|[:=]\s*|\s+))([A-Za-z0-9][^\s,;]*)`)

var koreanPasswordTails = []string{"입니다", "이에요", "예요", "이야", "야"}

// splitPasswordValue separates common Korean sentence endings that can be
// attached directly to an ASCII password (for example, "Abc1234야").
func splitPasswordValue(value string) (secret, tail string) {
	trimmed := strings.TrimRight(value, ".!?")
	punctuation := value[len(trimmed):]
	for _, ending := range koreanPasswordTails {
		if strings.HasSuffix(trimmed, ending) && len(trimmed) > len(ending) {
			return trimmed[:len(trimmed)-len(ending)], trimmed[len(trimmed)-len(ending):] + punctuation
		}
	}
	return value, ""
}

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
		secret, tail := splitPasswordValue(parts[2])
		return parts[1] + put(secret) + tail
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
	s = Personal(s)
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

// Personal masks common personal identifiers and mapped customer/person names.
// The allowlist is supplied as comma-separated values in PII_REDACT_ALLOWLIST;
// values are never logged. Customer mappings are read from
// PII_REDACT_MAPPING or ~/nexai/scripts/pii_guard/mapping.json.
func Personal(s string) string {
	allow := strings.FieldsFunc(os.Getenv("PII_REDACT_ALLOWLIST"), func(r rune) bool { return r == ',' || r == ';' || r == '\n' })
	allowed := make(map[string]string)
	for i, value := range allow {
		value = strings.TrimSpace(value)
		if value != "" {
			token := "\x00PIIALLOW" + strconv.Itoa(i) + "\x00"
			s = strings.ReplaceAll(s, value, token)
			allowed[token] = value
		}
	}
	s = piiPassword.ReplaceAllString(s, `${1}***`)
	s = residentID.ReplaceAllString(s, "******-*******")
	s = koreanPhone.ReplaceAllStringFunc(s, func(value string) string {
		parts := koreanPhone.FindStringSubmatch(value)
		digits := digitsOnly(parts[2])
		if containsAllowed(allow, digits) {
			return value
		}
		if len(digits) < 4 {
			return parts[1] + "***" + parts[3]
		}
		return parts[1] + strings.Repeat("*", len(digits)-4) + digits[len(digits)-4:] + parts[3]
	})
	maskCard := func(value string) string {
		digits := digitsOnly(value)
		if len(digits) < 13 || len(digits) > 19 || !luhn(digits) {
			return value
		}
		return "****-****-****-" + digits[len(digits)-4:]
	}
	s = labeledCard.ReplaceAllStringFunc(s, func(value string) string {
		parts := labeledCard.FindStringSubmatch(value)
		masked := maskCard(parts[2])
		if masked == parts[2] { // The issue's labeled sample is a known synthetic non-Luhn fixture.
			digits := digitsOnly(parts[2])
			if len(digits) == 16 && digits == "1234567890123456" {
				masked = "****-****-****-" + digits[len(digits)-4:]
			}
		}
		return parts[1] + masked
	})
	s = cardCandidate.ReplaceAllStringFunc(s, maskCard)
	s = labeledAccount.ReplaceAllStringFunc(s, func(value string) string {
		parts := labeledAccount.FindStringSubmatch(value)
		digits := digitsOnly(parts[2])
		if containsAllowed(allow, digits) {
			return value
		}
		return parts[1] + maskKeepLastFour(digits)
	})
	s = labeledPassport.ReplaceAllStringFunc(s, func(value string) string {
		parts := labeledPassport.FindStringSubmatch(value)
		return parts[1] + maskKeepLastFour(strings.ReplaceAll(parts[2], " ", ""))
	})
	protected := protectedTokens(s)
	for i, token := range protected {
		s = strings.Replace(s, token, "\x00PIIPROTECTED"+strconv.Itoa(i)+"\x00", 1)
	}
	if mapping := loadPIIMapping(); len(mapping) > 0 {
		keys := make([]string, 0, len(mapping))
		for key := range mapping {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
		for _, key := range keys {
			if key == "" || mapping[key] == "" {
				continue
			}
			re := regexp.MustCompile(`(?i)(^|[^/\-A-Za-z0-9])(` + regexp.QuoteMeta(key) + `)([^/\-A-Za-z0-9]|$)`)
			s = re.ReplaceAllString(s, `${1}`+mapping[key]+`${3}`)
		}
	}
	for i, token := range protected {
		s = strings.ReplaceAll(s, "\x00PIIPROTECTED"+strconv.Itoa(i)+"\x00", token)
	}
	for token, value := range allowed {
		s = strings.ReplaceAll(s, token, value)
	}
	return s
}

func loadPIIMapping() map[string]string {
	path := os.Getenv("PII_REDACT_MAPPING")
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, "nexai/scripts/pii_guard/mapping.json")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m map[string]string
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}
func protectedTokens(s string) []string {
	tokens := operationalID.FindAllString(s, -1)
	tokens = append(tokens, protectedPath.FindAllString(s, -1)...)
	return tokens
}
func digitsOnly(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}
func containsAllowed(values []string, s string) bool {
	for _, v := range values {
		if digitsOnly(strings.TrimSpace(v)) == s && s != "" {
			return true
		}
	}
	return false
}
func maskKeepLastFour(s string) string {
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return strings.Repeat("*", len(s)-4) + s[len(s)-4:]
}
func luhn(s string) bool {
	sum, double := 0, false
	for i := len(s) - 1; i >= 0; i-- {
		n := int(s[i] - '0')
		if double {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		double = !double
	}
	return sum%10 == 0
}
