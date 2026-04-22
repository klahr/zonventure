// Package httpx has tiny helpers for JSON request/response handling so
// handlers don't need to repeat the same encoding boilerplate.
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
)

// JSON writes v as JSON with the given status code.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("httpx: encode: %v", err)
	}
}

// Error writes a JSON error object with the given status and message.
func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}

// DecodeJSON decodes a JSON request body into dst, enforcing a 1 MiB limit
// and rejecting unknown fields. Returns a user-safe error string via Error
// on failure (the caller should still return after calling this).
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			Error(w, http.StatusBadRequest, "empty request body")
		} else {
			Error(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		}
		return false
	}
	return true
}
