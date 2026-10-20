package codesessions

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
)

func streamMemoryToolResponse(ctx context.Context, w http.ResponseWriter, body io.Reader, call sandboxRPCRequest, commit *memoryToolCommit) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 32<<10), 8<<20)
	scanner.Split(scanMemoryEventLine)
	var frame bytes.Buffer
	var confirmationErr error
	controller := http.NewResponseController(w)
	for scanner.Scan() {
		line := scanner.Text()
		if frame.Len()+len(line) > 8<<20 {
			return io.ErrUnexpectedEOF
		}
		frame.WriteString(line)
		if strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r") != "" {
			continue
		}
		updated, err := confirmMemoryToolEvent(ctx, frame.Bytes(), call, commit)
		confirmationErr = errors.Join(confirmationErr, err)
		if _, err := w.Write(updated); err != nil {
			return err
		}
		if err := controller.Flush(); err != nil {
			return err
		}
		frame.Reset()
	}
	if frame.Len() > 0 {
		updated, err := confirmMemoryToolEvent(ctx, frame.Bytes(), call, commit)
		confirmationErr = errors.Join(confirmationErr, err)
		if _, err := w.Write(updated); err != nil {
			return err
		}
	}
	return errors.Join(confirmationErr, scanner.Err())
}

func confirmMemoryToolEvent(ctx context.Context, frame []byte, call sandboxRPCRequest, commit *memoryToolCommit) ([]byte, error) {
	var data []string
	for _, line := range strings.Split(string(frame), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSuffix(line, "\r"), "data:"); ok {
			data = append(data, strings.TrimPrefix(value, " "))
		}
	}
	payload := []byte(strings.Join(data, "\n"))
	confirmed, err := commit.confirm(ctx, call, payload)
	if err == nil {
		return frame, nil
	}
	return append(append([]byte("event: message\ndata: "), confirmed...), []byte("\n\n")...), err
}

func scanMemoryEventLine(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i+1], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}
