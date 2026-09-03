package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Per-issue metadata is a small JSONB KV map agents use to record pipeline
// state (PR number, pipeline_status, waiting_on, ...). Three rules govern
// the V1 surface — they're enforced both in the handler and at the DB:
//
//   - keys match `^[a-zA-Z_][a-zA-Z0-9_.-]{0,63}$` (handler)
//   - at most 50 keys per issue (handler)
//   - values are primitive: string / number / bool (handler)
//   - JSONB column is an object and ≤ 8KB (DB CHECK; defense in depth)
//
// All mutations are single-key atomic. UpdateIssue does NOT touch metadata —
// any whole-blob overwrite would race with concurrent agent writes (see the
// design discussion on MUL-2017).
const (
	maxIssueMetadataKeys = 50
)

// requiredWaitingMetadataKeys are the ownership fields NEX-1043 introduces so
// "who currently holds the ball" and "what clears it" are machine-readable
// instead of living only in comment prose. waiting_on: "ceo" | "agent:<id>" |
// "external" | "event". unblock_condition: one observable sentence.
// waiting_since: RFC3339.
var requiredWaitingMetadataKeys = []string{"waiting_on", "unblock_condition", "waiting_since"}

// transitionsRequiringWaitingMetadata is intentionally blocked-only, not
// blocked+in_review. Every agent's standard runtime workflow ends every
// completed run with `issue status <id> in_review` (see the platform-wide
// assignment brief), so gating in_review here would 400 that closing call
// fleet-wide the moment this ships, for every workspace, before any agent
// template is updated to pre-set these keys. blocked is far rarer and
// already carried an informal waiting_on/blocked_reason convention (see
// missing-blocked-reason-means-bad-criteria), so it is the safe first step.
// Extending to in_review is a follow-up once the runtime brief sets these
// keys before closing a run.
var transitionsRequiringWaitingMetadata = map[string]bool{"blocked": true}

// transitionsWarnOnlyMissingWaitingMetadata are transitions where NEX-1043
// leaves a durable warning instead of rejecting the request outright.
// in_review is warn-only, not hard-gated like blocked: every agent's
// standard runtime workflow closes a completed run with `issue status <id>
// in_review`, so hard-gating it would 400 that closing call fleet-wide
// before any runtime template pre-sets these keys. The warning is written
// to issue metadata (see waitingMetadataWarningKey) rather than only
// returned in the HTTP response, so it survives the fire-and-forget CLI
// call that triggered it and stays queryable via `issue metadata list` /
// `issue list --metadata`.
var transitionsWarnOnlyMissingWaitingMetadata = map[string]bool{"in_review": true}

// waitingMetadataWarningKey holds a human-readable note recording that a
// warn-only transition happened without the three NEX-1043 ownership keys
// set. UpdateIssue clears it automatically the next time the issue makes a
// warn-gated transition with all three keys present.
const waitingMetadataWarningKey = "waiting_metadata_warning"

// missingWaitingMetadataKeys returns which of requiredWaitingMetadataKeys are
// absent from an issue's existing metadata, given the status it is
// transitioning to. Returns nil when the target status isn't gated or all
// keys are already present.
func missingWaitingMetadataKeys(targetStatus string, existing map[string]any) []string {
	if !transitionsRequiringWaitingMetadata[targetStatus] {
		return nil
	}
	return missingKeysOf(existing)
}

// missingWaitingMetadataKeysWarnOnly mirrors missingWaitingMetadataKeys but
// checks transitionsWarnOnlyMissingWaitingMetadata instead of the hard-gated
// set.
func missingWaitingMetadataKeysWarnOnly(targetStatus string, existing map[string]any) []string {
	if !transitionsWarnOnlyMissingWaitingMetadata[targetStatus] {
		return nil
	}
	return missingKeysOf(existing)
}

func missingKeysOf(existing map[string]any) []string {
	var missing []string
	for _, k := range requiredWaitingMetadataKeys {
		if _, ok := existing[k]; !ok {
			missing = append(missing, k)
		}
	}
	return missing
}

// waitingMetadataWarningNote formats the durable warning value written to
// waitingMetadataWarningKey when a warn-only transition proceeds with
// missing ownership keys.
func waitingMetadataWarningNote(targetStatus string, missing []string, at time.Time) string {
	return fmt.Sprintf("%s transition on %s missing metadata key(s): %s — set via `issue metadata set` (waiting_on, unblock_condition, waiting_since)",
		targetStatus, at.UTC().Format(time.RFC3339), strings.Join(missing, ", "))
}

var issueMetadataKeyRE = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.-]{0,63}$`)

// SetIssueMetadataKeyRequest carries the JSON value to write under the key
// named in the URL. Value is a RawMessage so we can preserve numeric vs.
// string typing through to PostgreSQL — once decoded into `any`, JSON
// numbers all collapse to float64 and we'd lose integer fidelity.
type SetIssueMetadataKeyRequest struct {
	Value json.RawMessage `json:"value"`
}

func validateIssueMetadataKey(key string) error {
	if key == "" {
		return errors.New("key is required")
	}
	if !issueMetadataKeyRE.MatchString(key) {
		return errors.New("key must match ^[a-zA-Z_][a-zA-Z0-9_.-]{0,63}$")
	}
	return nil
}

// validateIssueMetadataValue rejects anything other than a primitive JSON
// scalar. Null, arrays, and objects are not allowed — the V1 surface is
// flat KV. Removing a key uses DELETE, not a null value.
func validateIssueMetadataValue(raw json.RawMessage) error {
	if len(raw) == 0 {
		return errors.New("value is required")
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return fmt.Errorf("value must be valid JSON: %w", err)
	}
	switch v.(type) {
	case string, bool, float64:
		return nil
	case nil:
		return errors.New("value cannot be null (use DELETE to remove a key)")
	default:
		return errors.New("value must be a primitive: string, number, or bool")
	}
}

// parseIssueMetadata decodes the JSONB bytes from db.Issue.Metadata into a
// Go map suitable for response serialization. Empty or unparseable blobs
// degrade to an empty map — the DB CHECK guarantees object shape, so this
// path is only hit on rows somehow predating the migration.
func parseIssueMetadata(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

// parseMetadataFilterParam reads the `metadata` query parameter (a JSON
// object) and returns it as the JSONB filter blob passed to ListIssues /
// CountIssues / ListOpenIssues. Empty input means "no filter" and returns
// a nil []byte, which the SQL layer interprets as "skip the @> check".
//
// Validates that the filter is itself a flat object of primitives, mirroring
// the constraints we apply at write time — querying for `{key: {nested}}`
// would never match since written values are primitive by construction.
func parseMetadataFilterParam(w http.ResponseWriter, raw string) ([]byte, bool) {
	if raw == "" {
		return nil, true
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		writeError(w, http.StatusBadRequest, "metadata filter must be a JSON object")
		return nil, false
	}
	for k, v := range parsed {
		if err := validateIssueMetadataKey(k); err != nil {
			writeError(w, http.StatusBadRequest, "metadata filter "+err.Error())
			return nil, false
		}
		switch v.(type) {
		case string, bool, float64:
			// ok
		default:
			writeError(w, http.StatusBadRequest, "metadata filter values must be primitives (string, number, bool)")
			return nil, false
		}
	}
	// Re-marshal so we send canonical JSON to PG (and not the raw, possibly
	// whitespace-padded user input).
	buf, err := json.Marshal(parsed)
	if err != nil {
		writeError(w, http.StatusBadRequest, "metadata filter is invalid")
		return nil, false
	}
	return buf, true
}

func (h *Handler) ListIssueMetadata(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"metadata": parseIssueMetadata(issue.Metadata)})
}

func (h *Handler) SetIssueMetadataKey(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	key := chi.URLParam(r, "key")
	if err := validateIssueMetadataKey(key); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var req SetIssueMetadataKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validateIssueMetadataValue(req.Value); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	if !h.authorizeResource(w, r, uuidToString(issue.WorkspaceID), ResourceIssue, "mutate", resourceOwner{CreatorType: issue.CreatorType, CreatorID: uuidToString(issue.CreatorID)}) {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Enforce the key-count cap in the handler. The DB only guards size,
	// and a clear 4xx for "too many keys" beats a CHECK violation that
	// happens to fire on the size cap once enough keys accumulate.
	existing := parseIssueMetadata(issue.Metadata)
	if _, present := existing[key]; !present && len(existing) >= maxIssueMetadataKeys {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("metadata cannot exceed %d keys", maxIssueMetadataKeys))
		return
	}

	updated, err := h.Queries.SetIssueMetadataKey(r.Context(), db.SetIssueMetadataKeyParams{
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Key:         key,
		Value:       []byte(req.Value),
	})
	if err != nil {
		if isCheckViolation(err) {
			writeError(w, http.StatusBadRequest, "metadata exceeds the 8KB size limit")
			return
		}
		slog.Warn("SetIssueMetadataKey failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID, "key", key)...)
		writeError(w, http.StatusInternalServerError, "failed to set metadata key")
		return
	}

	workspaceID := uuidToString(updated.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	metadata := parseIssueMetadata(updated.Metadata)
	h.publish(protocol.EventIssueMetadataChanged, workspaceID, actorType, actorID, map[string]any{
		"issue_id": uuidToString(updated.ID),
		"metadata": metadata,
	})
	writeJSON(w, http.StatusOK, map[string]any{"metadata": metadata})
}

func (h *Handler) DeleteIssueMetadataKey(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	key := chi.URLParam(r, "key")
	if err := validateIssueMetadataKey(key); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}
	if !h.authorizeResource(w, r, uuidToString(issue.WorkspaceID), ResourceIssue, "mutate", resourceOwner{CreatorType: issue.CreatorType, CreatorID: uuidToString(issue.CreatorID)}) {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// NEX-1043: deleting one of the ownership keys is otherwise
	// indistinguishable from a transition that never set it — an issue
	// backfilled while blocked/in_review can be silently stripped back to
	// non-conforming by a later, unrelated metadata cleanup without ever
	// touching status. Apply the same rule DeleteIssueMetadataKey's sibling
	// (the status-transition gate in issue.go) already applies at the
	// transition itself: hard-reject the delete for statuses that require
	// these keys, warn (durably, via waitingMetadataWarningKey) for statuses
	// that only warn.
	if isRequiredWaitingMetadataKey(key) {
		if transitionsRequiringWaitingMetadata[issue.Status] {
			writeError(w, http.StatusBadRequest, fmt.Sprintf(
				"cannot delete %q while status is %q: NEX-1043 requires waiting_on/unblock_condition/waiting_since to stay set for this status (replace the value instead, e.g. via `issue metadata set`)",
				key, issue.Status,
			))
			return
		}
	}

	updated, err := h.Queries.DeleteIssueMetadataKey(r.Context(), db.DeleteIssueMetadataKeyParams{
		ID:          issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Key:         key,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
		slog.Warn("DeleteIssueMetadataKey failed", append(logger.RequestAttrs(r), "error", err, "issue_id", issueID, "key", key)...)
		writeError(w, http.StatusInternalServerError, "failed to delete metadata key")
		return
	}

	if isRequiredWaitingMetadataKey(key) && transitionsWarnOnlyMissingWaitingMetadata[issue.Status] {
		if missing := missingKeysOf(parseIssueMetadata(updated.Metadata)); len(missing) > 0 {
			noteJSON, _ := json.Marshal(waitingMetadataWarningNote(issue.Status, missing, time.Now()))
			if warned, werr := h.Queries.SetIssueMetadataKey(r.Context(), db.SetIssueMetadataKeyParams{
				ID: updated.ID, WorkspaceID: updated.WorkspaceID, Key: waitingMetadataWarningKey, Value: noteJSON,
			}); werr != nil {
				slog.Warn("failed to record waiting-metadata warning on delete", append(logger.RequestAttrs(r), "error", werr, "issue_id", issueID, "key", key)...)
			} else {
				updated = warned
			}
		}
	}

	workspaceID := uuidToString(updated.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	metadata := parseIssueMetadata(updated.Metadata)
	h.publish(protocol.EventIssueMetadataChanged, workspaceID, actorType, actorID, map[string]any{
		"issue_id": uuidToString(updated.ID),
		"metadata": metadata,
	})
	writeJSON(w, http.StatusOK, map[string]any{"metadata": metadata})
}

// isRequiredWaitingMetadataKey reports whether key is one of the three
// NEX-1043 ownership fields (waiting_on, unblock_condition, waiting_since).
func isRequiredWaitingMetadataKey(key string) bool {
	for _, k := range requiredWaitingMetadataKeys {
		if k == key {
			return true
		}
	}
	return false
}
