package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type Error struct {
	Status  int
	Type    string
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Type, e.Message)
}

func NewError(status int, typ, message string) *Error {
	return &Error{Status: status, Type: typ, Message: message}
}

func WriteError(w http.ResponseWriter, r *http.Request, err *Error) {
	WriteJSON(w, err.Status, map[string]any{
		"type":       "error",
		"request_id": RequestID(r.Context()),
		"error": map[string]string{
			"type":    err.Type,
			"message": err.Message,
		},
	})
}

func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
