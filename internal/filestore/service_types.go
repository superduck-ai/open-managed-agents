package filestore

import "io"

type readFileResult struct {
	Body      io.ReadCloser
	Size      int64
	MediaType string
}
