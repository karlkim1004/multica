package handler

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestValidationAutoCloseReasonsDefaultDenyAndGates(t *testing.T) {
	implementation := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	issue := db.Issue{Status: "in_review", AutoCloseAllowed: true, ImplementationAgentID: implementation, CurrentRef: pgtype.Text{String: "abc", Valid: true}, AutoCloseCriteriaVersion: pgtype.Text{String: "v1", Valid: true}}
	verdict := db.Comment{VerifiedRef: pgtype.Text{String: "abc", Valid: true}, CriteriaVersion: pgtype.Text{String: "v1", Valid: true}}
	if got := validationAutoCloseReasons(issue, verdict, "00000000-0000-0000-0000-000000000002", 0, 0); len(got) != 0 {
		t.Fatalf("safe verdict rejected: %v", got)
	}
	sameVerifier := issue
	sameVerifier.ImplementationAgentID = pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	if got := validationAutoCloseReasons(sameVerifier, verdict, "00000000-0000-0000-0000-000000000002", 0, 0); !strings.Contains(strings.Join(got, " "), "independent") {
		t.Fatalf("same verifier was not rejected: %v", got)
	}

	cases := []struct {
		name          string
		mutate        func(*db.Issue, *db.Comment)
		prs, children int64
		want          string
	}{
		{"policy", func(i *db.Issue, _ *db.Comment) { i.AutoCloseAllowed = false }, 0, 0, "auto_close_allowed"},
		{"missing implementation", func(i *db.Issue, _ *db.Comment) { i.ImplementationAgentID = pgtype.UUID{} }, 0, 0, "independent"},
		{"stale ref", func(_ *db.Issue, c *db.Comment) { c.VerifiedRef.String = "old" }, 0, 0, "verified_ref"},
		{"stale criteria", func(_ *db.Issue, c *db.Comment) { c.CriteriaVersion.String = "v0" }, 0, 0, "criteria_version"},
		{"external", func(i *db.Issue, _ *db.Comment) { i.ExternalValidationRequired = true }, 0, 0, "external validation"},
		{"open pr", func(_ *db.Issue, _ *db.Comment) {}, 1, 0, "linked PR"},
		{"open child", func(_ *db.Issue, _ *db.Comment) {}, 0, 1, "child issue"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i, c := issue, verdict
			tc.mutate(&i, &c)
			got := validationAutoCloseReasons(i, c, "different", tc.prs, tc.children)
			if !strings.Contains(strings.Join(got, " "), tc.want) {
				t.Fatalf("%s gate missing: %v", tc.want, got)
			}
		})
	}
}
