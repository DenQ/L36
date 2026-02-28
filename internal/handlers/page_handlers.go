package handlers

import (
	"encoding/json"
	"l36/internal/storage"
	"net/http"
)

func RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/pages", CreatePageHandler)
	mux.HandleFunc("DELETE /api/pages/{pid}", DeletePageHandler)
	mux.HandleFunc("POST /api/pages/{pid}/versions", AddVersionHandler)
	mux.HandleFunc("GET /api/pages/{pid}/versions", GetHistoryHandler)
	mux.HandleFunc("GET /api/pages/{pid}/versions/{vid}", GetVersionHandler) // мы почему-то заходим сюда
	mux.HandleFunc("POST /api/pages/{pid}/versions/{vid}/latest", SetLatestHandler)
	mux.HandleFunc("GET /api/pages/{pid}/versions/latest", GetLatestVersionHandler) // хотя должны сюда
}

func CreatePageHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var input struct {
		Content any    `json:"content"`
		PageId  string `json:"pageId"`
	}

	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "Invalid input: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Гарантируем, что content — это строка, и не "null"
	var contentStr string
	if str, ok := input.Content.(string); ok {
		contentStr = str
	} else if input.Content == nil {
		contentStr = ""
	} else {
		b, _ := json.Marshal(input.Content)
		contentStr = string(b)
		if contentStr == "null" {
			contentStr = ""
		}
	}

	page := storage.GPageStorage.CreatePage(input.PageId, contentStr)

	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(page); err != nil {
		http.Error(w, "JSON encoding error", http.StatusInternalServerError)
		return
	}
}

func DeletePageHandler(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("pid")
	if pid == "" {
		http.Error(w, "Missing page ID", http.StatusBadRequest)
		return
	}

	if ok := storage.GPageStorage.DeletePage(pid); ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	http.Error(w, "Page not found", http.StatusNotFound)
}

func AddVersionHandler(w http.ResponseWriter, r *http.Request) {
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

	var contentStr string
	if str, ok := input.Content.(string); ok {
		contentStr = str
	} else if input.Content == nil {
		contentStr = ""
	} else {
		b, _ := json.Marshal(input.Content)
		contentStr = string(b)
		if contentStr == "null" {
			contentStr = ""
		}
	}

	newVer, ok := storage.GPageStorage.AddVersion(pid, contentStr)
	if !ok {
		http.Error(w, "Page not found", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(newVer)
}

func GetHistoryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	pid := r.PathValue("pid")
	if pid == "" {
		http.Error(w, "Missing page ID", http.StatusBadRequest)
		return
	}

	history, ok := storage.GPageStorage.GetHistory(pid)
	if !ok {
		http.Error(w, "Page not found", http.StatusNotFound)
		return
	}

	if err := json.NewEncoder(w).Encode(history); err != nil {
		http.Error(w, "JSON encoding error", http.StatusInternalServerError)
		return
	}
}

func GetVersionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	pid := r.PathValue("pid")
	vid := r.PathValue("vid")

	if pid == "" || vid == "" {
		http.Error(w, "Missing Page or Version ID", http.StatusBadRequest)
		return
	}

	version, ok := storage.GPageStorage.GetVersion(pid, vid)
	if !ok {
		http.Error(w, "33 Version or Page not found", http.StatusNotFound)
		return
	}

	if err := json.NewEncoder(w).Encode(version); err != nil {
		http.Error(w, "JSON encoding error", http.StatusInternalServerError)
		return
	}
}

func SetLatestHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	pid := r.PathValue("pid")
	vid := r.PathValue("vid")

	version, ok := storage.GPageStorage.SetLatest(pid, vid)
	if !ok {
		http.Error(w, "1 Version or Page not found", http.StatusNotFound)
		return
	}

	if err := json.NewEncoder(w).Encode(version); err != nil {
		http.Error(w, "JSON encoding error", http.StatusInternalServerError)
		return
	}
}

func GetLatestVersionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	pid := r.PathValue("pid")
	if pid == "" {
		http.Error(w, "Missing Page ID", http.StatusBadRequest)
		return
	}

	version, ok := storage.GPageStorage.GetLatestVersion(pid)
	if !ok {
		http.Error(w, "2 Latest version or Page not found", http.StatusNotFound)
		return
	}

	if err := json.NewEncoder(w).Encode(version); err != nil {
		http.Error(w, "JSON encoding error", http.StatusInternalServerError)
		return
	}
}
