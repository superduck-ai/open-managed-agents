package skills

import (
	"archive/zip"
	"fmt"
)

const maxSkillArchiveUncompressedBytes uint64 = 500 * 1024 * 1024

func addZipFileUncompressedSize(total uint64, file *zip.File) (uint64, error) {
	if file == nil {
		return total, nil
	}
	next := total + file.UncompressedSize64
	if next < total || next > maxSkillArchiveUncompressedBytes {
		return total, fmt.Errorf("skill archive uncompressed size exceeds %d bytes", maxSkillArchiveUncompressedBytes)
	}
	return next, nil
}
