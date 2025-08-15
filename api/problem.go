package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
)

type Problem struct {
	Type   string         `json:"type"`
	Title  string         `json:"title"`
	Status int            `json:"status"`
	Code   string         `json:"code"`
	Detail string         `json:"detail,omitempty"`
	Meta   map[string]any `json:"meta,omitempty"`
	RID    string         `json:"rid"`
}

func newRID(w http.ResponseWriter) string {
	rid := uuid.NewString()
	w.Header().Set("X-Request-ID", rid)
	return rid
}

func writeProblem(w http.ResponseWriter, rid string, status int, code, title, detail string, meta map[string]any) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Problem{
		Type:   "about:blank",
		Title:  title,
		Status: status,
		Code:   code,
		Detail: detail,
		Meta:   meta,
		RID:    rid,
	})
}
