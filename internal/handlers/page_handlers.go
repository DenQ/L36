package handlers

import (
	"encoding/json"
	"errors"
	"l36/internal/models"
	"l36/internal/storage"
	"net/http"
)

type Handler struct {
	store *storage.PageStorage
}

func RegisterRoutes(mux *http.ServeMux, store *storage.PageStorage) {
	h := &Handler{store: store}
	mux.HandleFunc("POST /api/pages", h.CreatePageHandler)
	mux.HandleFunc("DELETE /api/pages/{pid}", h.DeletePageHandler)
	mux.HandleFunc("POST /api/pages/{pid}/versions", h.AddVersionHandler)
	mux.HandleFunc("GET /api/pages/{pid}/versions", h.GetHistoryHandler)
	mux.HandleFunc("GET /api/pages/{pid}/versions/{vid}", h.GetVersionHandler)
	mux.HandleFunc("POST /api/pages/{pid}/versions/{vid}/latest", h.SetLatestHandler)
	mux.HandleFunc("GET /api/pages/{pid}/versions/latest", h.GetLatestVersionHandler)
}

func contentString(content any) string {
	if str, ok := content.(string); ok {
		return str
	}
	if content == nil {
		return ""
	}
	b, err := json.Marshal(content)
	if err != nil || string(b) == "null" {
		return ""
	}
	return string(b)
}

func (h *Handler) CreatePageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var input struct {
		Content any    `json:"content"`
		PageId  string `json:"pageId"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid input: "+err.Error(), http.StatusBadRequest)
		return
	}

	page, err := h.store.CreatePage(input.PageId, contentString(input.Content))
	if errors.Is(err, storage.ErrPageExists) {
		http.Error(w, "Page already exists", http.StatusConflict)
		return
	}
	if errors.Is(err, storage.ErrEmptyPageID) {
		http.Error(w, "Missing page ID", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(page); err != nil {
		http.Error(w, "JSON encoding error", http.StatusInternalServerError)
		return
	}
}

func (h *Handler) DeletePageHandler(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	if pid == "" {
		http.Error(w, "Missing page ID", http.StatusBadRequest)
		return
	}

	if ok := h.store.DeletePage(pid); ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	http.Error(w, "Page not found", http.StatusNotFound)
}

func (h *Handler) AddVersionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	pid := r.PathValue("pid")
	if pid == "" {
		http.Error(w, "Missing page ID", http.StatusBadRequest)
		return
	}

	var input struct {
		Content any `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	newVer, ok := h.store.AddVersion(pid, contentString(input.Content))
	if !ok {
		http.Error(w, "Page not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(newVer)
}

func (h *Handler) GetHistoryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	pid := r.PathValue("pid")
	if pid == "" {
		http.Error(w, "Missing page ID", http.StatusBadRequest)
		return
	}

	history, ok := h.store.GetHistory(pid)
	if !ok {
		http.Error(w, "Page not found", http.StatusNotFound)
		return
	}

	items := make([]models.VersionListItem, len(history))
	for i, v := range history {
		items[i] = models.VersionListItem{
			ID:        v.ID,
			ParentID:  v.ParentID,
			CreatedAt: v.CreatedAt,
			IsLatest:  v.IsLatest,
		}
	}
	if err := json.NewEncoder(w).Encode(items); err != nil {
		http.Error(w, "JSON encoding error", http.StatusInternalServerError)
		return
	}
}

func (h *Handler) GetVersionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	pid := r.PathValue("pid")
	vid := r.PathValue("vid")

	if pid == "" || vid == "" {
		http.Error(w, "Missing Page or Version ID", http.StatusBadRequest)
		return
	}

	version, err := h.store.GetVersion(pid, vid)
	if errors.Is(err, storage.ErrVersionNotFound) {
		http.Error(w, "Version not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Failed to restore version", http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(version); err != nil {
		http.Error(w, "JSON encoding error", http.StatusInternalServerError)
		return
	}
}

func (h *Handler) SetLatestHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	pid := r.PathValue("pid")
	vid := r.PathValue("vid")

	version, ok := h.store.SetLatest(pid, vid)
	if !ok {
		http.Error(w, "Version not found", http.StatusNotFound)
		return
	}

	if err := json.NewEncoder(w).Encode(version); err != nil {
		http.Error(w, "JSON encoding error", http.StatusInternalServerError)
		return
	}
}

func (h *Handler) GetLatestVersionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	pid := r.PathValue("pid")
	if pid == "" {
		http.Error(w, "Missing Page ID", http.StatusBadRequest)
		return
	}

	version, err := h.store.GetLatestVersion(pid)
	if errors.Is(err, storage.ErrVersionNotFound) {
		http.Error(w, "Version not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Failed to restore version", http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(version); err != nil {
		http.Error(w, "JSON encoding error", http.StatusInternalServerError)
		return
	}
}
