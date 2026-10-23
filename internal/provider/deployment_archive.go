// SPDX-License-Identifier: MPL-2.0
package provider

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path"
	"strings"

	"github.com/Edson-Edwin/terraform-provider-catalyst/internal/catalyst"
)

func deploymentArchiveFiles(content []byte) (map[string]*zip.File, error) {
	archive, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, errors.New("source must be a valid ZIP archive")
	}
	files := make(map[string]*zip.File)
	var total uint64
	for _, file := range archive.File {
		if path.Clean(file.Name) != strings.TrimSuffix(file.Name, "/") || strings.Contains(file.Name, "\\") || strings.HasPrefix(file.Name, "/") || path.Clean(file.Name) == ".." || strings.HasPrefix(path.Clean(file.Name), "../") || (!file.Mode().IsRegular() && !file.FileInfo().IsDir()) || file.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("archive contains an unsafe entry")
		}
		if file.FileInfo().IsDir() {
			continue
		}
		if files[file.Name] != nil {
			return nil, errors.New("archive contains duplicate entries")
		}
		files[file.Name] = file
		if file.UncompressedSize64 > uint64(catalyst.MaxFileBytes)-total {
			return nil, errors.New("expanded deployment archive exceeds 100 MB")
		}
		total += file.UncompressedSize64
	}
	for _, file := range files {
		reader, err := file.Open()
		if err != nil {
			return nil, errors.New("archive entry could not be opened")
		}
		size, readErr := io.Copy(io.Discard, io.LimitReader(reader, int64(file.UncompressedSize64)+1))
		_ = reader.Close()
		if readErr != nil || uint64(size) != file.UncompressedSize64 {
			return nil, errors.New("archive contains corrupt entries")
		}
	}
	return files, nil
}
