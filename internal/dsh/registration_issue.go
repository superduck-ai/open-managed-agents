package dsh

import (
	"net/http"
	"time"
)

type RegistrationIssue struct {
	Status                int
	Complete              bool
	ResponseEpoch         int64
	StartedAt, FinishedAt time.Time
}
type registrationIssueWriter struct {
	http.ResponseWriter
	issue RegistrationIssue
	wrote bool
}

func (w *registrationIssueWriter) WriteHeader(status int) {
	if w.issue.Status == 0 {
		w.issue.Status = status
		w.issue.StartedAt = time.Now().UTC()
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *registrationIssueWriter) Write(raw []byte) (int, error) {
	if w.issue.Status == 0 {
		w.issue.Status = http.StatusOK
		w.issue.StartedAt = time.Now().UTC()
	}
	n, err := w.ResponseWriter.Write(raw)
	w.wrote = w.wrote || n > 0
	w.issue.Complete = w.issue.Complete && err == nil && n == len(raw)
	w.issue.FinishedAt = time.Now().UTC()
	return n, err
}
