package mcp

import (
	"io"
	"os"
	"path/filepath"
)

func sFileOpen(path string) (*os.File, error) {
	return os.Open(path)
}

func writeToFile(dst string, src io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, src)
	return err
}
