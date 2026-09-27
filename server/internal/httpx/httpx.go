// Package httpx — общие помощники HTTP: JSON-ответы и ошибки в формате docs/07-api.md.
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
)

type errorBody struct {
	Error   string            `json:"error"`
	Message string            `json:"message,omitempty"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Debug("write json", "err", err)
	}
}

func OK(w http.ResponseWriter, v any) { JSON(w, http.StatusOK, v) }

func Error(w http.ResponseWriter, status int, code, msg string) {
	JSON(w, status, errorBody{Error: code, Message: msg})
}

func FieldErrors(w http.ResponseWriter, fields map[string]string) {
	JSON(w, http.StatusUnprocessableEntity, errorBody{Error: "validation", Message: "invalid values", Fields: fields})
}

// Internal пишет ошибку в лог и отвечает 500 без подробностей.
func Internal(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	Error(w, http.StatusInternalServerError, "internal", "internal error")
}

// Decode читает JSON-тело не больше maxBytes. При ошибке сам отвечает 400 и возвращает false.
func Decode(w http.ResponseWriter, r *http.Request, v any, maxBytes int64) bool {
	body := http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(body)
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			Error(w, http.StatusRequestEntityTooLarge, "too_large", "request body too large")
		} else if errors.Is(err, io.EOF) {
			Error(w, http.StatusBadRequest, "bad_request", "empty body")
		} else {
			Error(w, http.StatusBadRequest, "bad_request", err.Error())
		}
		return false
	}
	return true
}
