package pricing

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const maxSnapshotBytes = 64 << 20

func (s *Store) Reload() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	file, err := os.Open(s.config.SnapshotFile)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxSnapshotBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxSnapshotBytes {
		return errors.New("价格文件超过大小限制")
	}
	next, err := decodeSnapshot(data)
	if err != nil {
		return err
	}
	s.current.Store(next)
	return nil
}

func writeSnapshot(path string, data []byte) error {
	if len(data) > maxSnapshotBytes {
		return errors.New("价格文件超过大小限制")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".model-prices-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err := io.Copy(file, bytes.NewReader(data)); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
