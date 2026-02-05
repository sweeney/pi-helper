// Package envwriter provides atomic writing of environment variable files.
package envwriter

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Writer defines the interface for writing environment variable files.
type Writer interface {
	Write(vars map[string]string) error
}

// FileWriter writes environment variables to a file atomically.
type FileWriter struct {
	path string
}

// New creates a new FileWriter that writes to the specified path.
func New(path string) *FileWriter {
	return &FileWriter{path: path}
}

// Write writes the given environment variables to the file atomically.
// It writes to a temporary file first, then renames it to the target path.
func (w *FileWriter) Write(vars map[string]string) error {
	content := formatEnvVars(vars)

	dir := filepath.Dir(w.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	tmpFile, err := os.CreateTemp(dir, ".pi-helper-env-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Make file world-readable so other services can consume it
	if err := os.Chmod(tmpPath, 0644); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to chmod temp file: %w", err)
	}

	if err := os.Rename(tmpPath, w.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temp file to %s: %w", w.path, err)
	}

	return nil
}

// Path returns the path this writer writes to.
func (w *FileWriter) Path() string {
	return w.path
}

// formatEnvVars formats environment variables as KEY=value lines.
// Keys are sorted alphabetically for consistent output.
func formatEnvVars(vars map[string]string) string {
	if len(vars) == 0 {
		return ""
	}

	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(vars[k])
		sb.WriteString("\n")
	}
	return sb.String()
}
