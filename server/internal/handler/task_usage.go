package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TaskUsageResponse preserves provider/model detail and omits storage metadata.
type TaskUsageResponse struct {
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
}

func taskUsageToResponse(row db.TaskUsage) TaskUsageResponse {
	return TaskUsageResponse{row.Provider, row.Model, row.InputTokens, row.OutputTokens, row.CacheReadTokens, row.CacheWriteTokens}
}

// GetTaskUsage returns reported usage only; an empty array means unreported.
func (h *Handler) GetTaskUsage(w http.ResponseWriter, r *http.Request) {
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "task_id")
	if !ok {
		return
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	wsID := h.TaskService.ResolveTaskWorkspaceID(r.Context(), task)
	if wsID == "" || wsID != middleware.WorkspaceIDFromContext(r.Context()) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	rows, err := h.Queries.GetTaskUsage(r.Context(), task.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get task usage")
		return
	}
	resp := make([]TaskUsageResponse, len(rows))
	for i, row := range rows {
		resp[i] = taskUsageToResponse(row)
	}
	writeJSON(w, http.StatusOK, resp)
}
