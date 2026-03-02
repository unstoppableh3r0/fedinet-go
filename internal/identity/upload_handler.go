package identity

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// UploadImageHandler handles POST /upload/image
// Accepts multipart/form-data with field "image" (max 10 MB).
// Returns JSON: { "url": "/uploads/<uuid>.<ext>" }
func UploadImageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		RespondWithError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// 10 MB limit
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		RespondWithError(w, http.StatusBadRequest, "file too large or invalid form")
		return
	}

	file, header, err := r.FormFile("image")
	if err != nil {
		RespondWithError(w, http.StatusBadRequest, "missing image field")
		return
	}
	defer file.Close()

	// Validate MIME type
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	contentType := http.DetectContentType(buf[:n])
	if !strings.HasPrefix(contentType, "image/") {
		RespondWithError(w, http.StatusBadRequest, "file must be an image")
		return
	}
	// Seek back to start
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to process file")
		return
	}

	// Determine extension from original filename
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		// Fall back to content-type derived extension
		switch contentType {
		case "image/jpeg":
			ext = ".jpg"
		case "image/png":
			ext = ".png"
		case "image/gif":
			ext = ".gif"
		case "image/webp":
			ext = ".webp"
		default:
			ext = ".bin"
		}
	}

	// Ensure uploads directory exists
	uploadDir := "./uploads"
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to create upload directory")
		return
	}

	// Save with a UUID filename
	filename := fmt.Sprintf("%s%s", uuid.New().String(), ext)
	destPath := filepath.Join(uploadDir, filename)

	dest, err := os.Create(destPath)
	if err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to save file")
		return
	}
	defer dest.Close()

	if _, err := io.Copy(dest, file); err != nil {
		RespondWithError(w, http.StatusInternalServerError, "failed to write file")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"url": "/uploads/" + filename,
	})
}
