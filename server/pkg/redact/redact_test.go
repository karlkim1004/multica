package redact

import (
	"os"
	"strings"
	"testing"
)

func TestReplaceSecretsAndProtectExistingReferences(t *testing.T) {
	cases := []struct{ input, want string }{
		{"네이버웍스 비밀번호 Abc1234 입니다", "네이버웍스 비밀번호 [secret:test-ref] 입니다"},
		{"패스워드는 Abc1234", "패스워드는 [secret:test-ref]"},
		{"암호는 Abc1234야", "암호는 [secret:test-ref]야"},
		{"비밀번호 변경 방법을 알려줘", "비밀번호 변경 방법을 알려줘"},
		{"password: [secret:123]", "password: [secret:123]"},
		{"pw는 X", "pw는 [secret:test-ref]"},
		{"password: X", "password: [secret:test-ref]"},
		{"token sk-12345678901234567890", "token [secret:test-ref]"},
		{"token ghp_12345678901234567890", "token [secret:test-ref]"},
		{"token eyJ123456789012345678901234567890", "token [secret:test-ref]"},
		{"-----BEGIN PRIVATE KEY-----\nsecret-data\n-----END PRIVATE KEY-----", "[secret:test-ref]"},
		{"일반적인 문장입니다", "일반적인 문장입니다"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ReplaceSecrets(tc.input, func(value string) (string, error) { return "test-ref", nil })
			if err != nil {
				t.Fatalf("ReplaceSecrets: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReplaceSecretsSeparatesKoreanPasswordSentenceEndings(t *testing.T) {
	cases := []struct {
		input, want, stored string
	}{
		{"비밀번호가 Abc1234야", "비밀번호가 [secret:test-ref]야", "Abc1234"},
		{"비밀번호가 Abc1234입니다.", "비밀번호가 [secret:test-ref]입니다.", "Abc1234"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			var stored string
			got, err := ReplaceSecrets(tc.input, func(value string) (string, error) {
				stored = value
				return "test-ref", nil
			})
			if err != nil {
				t.Fatalf("ReplaceSecrets: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if stored != tc.stored {
				t.Errorf("stored %q, want %q", stored, tc.stored)
			}
		})
	}
}

func TestReplaceSecretsFailsClosedWithoutStore(t *testing.T) {
	got, err := ReplaceSecrets("password: Abc1234", nil)
	if err == nil || got == "password: Abc1234" || strings.Contains(got, "Abc1234") {
		t.Fatalf("got %q, err %v; expected a masked result and an error", got, err)
	}
}

func TestPersonalDataRedactionAndProtectedIdentifiers(t *testing.T) {
	t.Setenv("PII_REDACT_MAPPING", "")
	input := "카드 1234-5678-9012-3456, 비밀번호는 Abc1234, 주민 900101-1234567, 전화 010-1234-5678, LG전자 담당자 OPS-0510-AI-FOO scripts/customer/LG.txt"
	got := Text(input)
	for _, want := range []string{"카드 ****-****-****-3456", "비밀번호는 ***", "******-*******", "*******5678", "A사 담당자", "OPS-0510-AI-FOO", "scripts/customer/LG.txt"} {
		if !strings.Contains(got, want) {
			t.Errorf("sanitized output missing %q: %q", want, got)
		}
	}
	if strings.Contains(got, "1234-5678-9012-3456") || strings.Contains(got, "Abc1234") {
		t.Fatalf("sensitive value survived: %q", got)
	}
}

func TestPersonalDataRedactionRejectsInvalidCardAndAllowsConfiguredPhone(t *testing.T) {
	t.Setenv("PII_REDACT_MAPPING", "")
	t.Setenv("PII_REDACT_ALLOWLIST", "010-1234-5678")
	input := "card 1234-5678-9012-3457 phone 010-1234-5678"
	got := Text(input)
	if !strings.Contains(got, "1234-5678-9012-3457") {
		t.Fatalf("invalid Luhn candidate was masked: %q", got)
	}
	if !strings.Contains(got, "010-1234-5678") {
		t.Fatalf("allowlisted phone was altered: %q", got)
	}
}

func TestPersonalDataRedactionPreservesTimestampsAndInvalidResidentIDs(t *testing.T) {
	t.Setenv("PII_REDACT_MAPPING", "")
	for _, value := range []string{"1729764000123", "1234567890123", "991332-1234567"} {
		if got := Personal(value); got != value {
			t.Errorf("non-PII numeric value %q changed to %q", value, got)
		}
	}
	if got := Personal("resident 900101-1234567"); !strings.Contains(got, "******-*******") {
		t.Fatalf("valid formatted resident ID not redacted: %q", got)
	}
}

func TestCustomerMappingUsesLongestKeyAndBoundaries(t *testing.T) {
	path := t.TempDir() + "/mapping.json"
	if err := os.WriteFile(path, []byte(`{"LG전자":"A사","LG":"A사","LGCOMMIE":"T-PROJECT"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PII_REDACT_MAPPING", path)
	got := Personal("LG전자 LGCOMMIE path/LG전자 OPS-1000-ABC")
	want := "A사 T-PROJECT path/LG전자 OPS-1000-ABC"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRedactAWSAccessKey(t *testing.T) {
	t.Parallel()
	input := "Found key AKIAIOSFODNN7EXAMPLE in config"
	got := Text(input)
	if strings.Contains(got, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("AWS key not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED AWS KEY]") {
		t.Fatalf("expected [REDACTED AWS KEY] placeholder, got: %s", got)
	}
}

func TestRedactAWSSecretKey(t *testing.T) {
	t.Parallel()
	input := "aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	got := Text(input)
	if strings.Contains(got, "wJalrXUtnFEMI") {
		t.Fatalf("AWS secret not redacted: %s", got)
	}
}

func TestRedactPrivateKey(t *testing.T) {
	t.Parallel()
	input := "Here is the key:\n-----BEGIN RSA PRIVATE KEY-----\nMIIEow...\n-----END RSA PRIVATE KEY-----\nDone."
	got := Text(input)
	if strings.Contains(got, "MIIEow") {
		t.Fatalf("private key content not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED PRIVATE KEY]") {
		t.Fatalf("expected [REDACTED PRIVATE KEY] placeholder, got: %s", got)
	}
}

func TestRedactGitHubToken(t *testing.T) {
	t.Parallel()
	input := "export GITHUB_TOKEN=ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmn"
	got := Text(input)
	if strings.Contains(got, "ghp_") {
		t.Fatalf("GitHub token not redacted: %s", got)
	}
}

func TestRedactOpenAIKey(t *testing.T) {
	t.Parallel()
	input := "OPENAI_API_KEY=sk-proj-abc123def456ghi789jkl012mno345"
	got := Text(input)
	if strings.Contains(got, "sk-proj-abc123") {
		t.Fatalf("OpenAI key not redacted: %s", got)
	}
}

func TestRedactSlackToken(t *testing.T) {
	t.Parallel()
	input := "token: xoxb-123456789012-1234567890123-AbCdEfGhIjKl"
	got := Text(input)
	if strings.Contains(got, "xoxb-") {
		t.Fatalf("Slack token not redacted: %s", got)
	}
}

func TestRedactBearerToken(t *testing.T) {
	t.Parallel()
	input := "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abc123"
	got := Text(input)
	if strings.Contains(got, "eyJhbGci") {
		t.Fatalf("Bearer token not redacted: %s", got)
	}
}

func TestRedactGenericCredentials(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
	}{
		{"API_KEY", "API_KEY=mysupersecretkey123"},
		{"DATABASE_URL", "DATABASE_URL=postgres://user:pass@host/db"},
		{"DB_PASSWORD", "DB_PASSWORD: hunter2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Text(tc.input)
			if !strings.Contains(got, "[REDACTED CREDENTIAL]") {
				t.Fatalf("expected credential redaction for %s, got: %s", tc.name, got)
			}
		})
	}
}

func TestRedactHomeDirectory(t *testing.T) {
	t.Parallel()
	if homeDir == "" || username == "" {
		t.Skip("cannot determine home dir or username")
	}
	input := "Reading file at " + homeDir + "/Documents/secret.txt"
	got := Text(input)
	if strings.Contains(got, username) {
		t.Fatalf("home directory username not redacted: %s", got)
	}
	if !strings.Contains(got, "****") {
		t.Fatalf("expected **** in path, got: %s", got)
	}
}

func TestNoFalsePositivesOnNormalText(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"This is a normal commit message about fixing a bug",
		"The function returns skip-navigation as the class name",
		"Created PR #42 for the authentication feature",
		"Running tests in /tmp/test-workspace/project",
		"The API endpoint /api/issues/123 was updated",
	}
	for _, input := range inputs {
		got := Text(input)
		if got != input {
			t.Fatalf("false positive redaction:\n  input:  %s\n  output: %s", input, got)
		}
	}
}

func TestRedactGitLabToken(t *testing.T) {
	t.Parallel()
	input := "GITLAB_TOKEN=glpat-AbCdEfGhIjKlMnOpQrStUvWx"
	got := Text(input)
	if strings.Contains(got, "glpat-") {
		t.Fatalf("GitLab token not redacted: %s", got)
	}
}

func TestRedactJWT(t *testing.T) {
	t.Parallel()
	input := "token: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	got := Text(input)
	if strings.Contains(got, "eyJhbGci") {
		t.Fatalf("JWT not redacted: %s", got)
	}
}

func TestRedactConnectionString(t *testing.T) {
	t.Parallel()
	input := "connecting to postgres://admin:s3cret@db.example.com:5432/mydb"
	got := Text(input)
	if strings.Contains(got, "s3cret") {
		t.Fatalf("connection string password not redacted: %s", got)
	}
}

func TestRedactPasswordEnvVar(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
	}{
		{"PASSWORD", "PASSWORD=hunter2"},
		{"SECRET", "SECRET=mysecretvalue"},
		{"TOKEN", "TOKEN=abc123xyz"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Text(tc.input)
			if !strings.Contains(got, "[REDACTED CREDENTIAL]") {
				t.Fatalf("expected credential redaction for %s, got: %s", tc.name, got)
			}
		})
	}
}

func TestInputMap(t *testing.T) {
	t.Parallel()
	m := map[string]any{
		"command":   "echo sk-proj-abc123def456ghi789jkl012mno345",
		"file_path": "/tmp/test.txt",
		"count":     42,
	}
	got := InputMap(m)
	if s, ok := got["command"].(string); ok {
		if strings.Contains(s, "sk-proj") {
			t.Fatalf("API key in input map not redacted: %s", s)
		}
	}
	// Non-string values preserved
	if got["count"] != 42 {
		t.Fatalf("non-string value altered: %v", got["count"])
	}
	// Clean strings unchanged
	if got["file_path"] != "/tmp/test.txt" {
		t.Fatalf("clean string altered: %v", got["file_path"])
	}
}

func TestInputMapNil(t *testing.T) {
	t.Parallel()
	if got := InputMap(nil); got != nil {
		t.Fatalf("expected nil, got: %v", got)
	}
}

func TestRedactMultipleSecrets(t *testing.T) {
	t.Parallel()
	input := "Keys: AKIAIOSFODNN7EXAMPLE and ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmn"
	got := Text(input)
	if strings.Contains(got, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatal("AWS key not redacted in multi-secret text")
	}
	if strings.Contains(got, "ghp_") {
		t.Fatal("GitHub token not redacted in multi-secret text")
	}
}
