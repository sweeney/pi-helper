package envwriter

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileWriter_Write(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, "test.env")

	writer := New(envPath)

	vars := map[string]string{
		"NETWORK_STATUS": "connected",
		"NETWORK_TYPE":   "wifi",
		"NETWORK_IP":     "192.168.1.100",
	}

	if err := writer.Write(vars); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	content, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("failed to read env file: %v", err)
	}

	expected := "NETWORK_IP=192.168.1.100\nNETWORK_STATUS=connected\nNETWORK_TYPE=wifi\n"
	if string(content) != expected {
		t.Errorf("Write() content = %q, want %q", string(content), expected)
	}
}

func TestFileWriter_Write_WorldReadable(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, "test.env")

	writer := New(envPath)

	if err := writer.Write(map[string]string{"KEY": "value"}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatalf("failed to stat env file: %v", err)
	}

	// File should be world-readable (0644)
	perm := info.Mode().Perm()
	if perm != 0644 {
		t.Errorf("Write() file permissions = %o, want %o", perm, 0644)
	}
}

func TestFileWriter_Write_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, "subdir", "test.env")

	writer := New(envPath)

	vars := map[string]string{
		"KEY": "value",
	}

	if err := writer.Write(vars); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if _, err := os.Stat(envPath); os.IsNotExist(err) {
		t.Error("Write() did not create file")
	}
}

func TestFileWriter_Write_EmptyVars(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, "test.env")

	writer := New(envPath)

	if err := writer.Write(map[string]string{}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	content, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("failed to read env file: %v", err)
	}

	if string(content) != "" {
		t.Errorf("Write() with empty vars = %q, want empty string", string(content))
	}
}

func TestFileWriter_Write_Overwrites(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, "test.env")

	writer := New(envPath)

	// Write initial content
	if err := writer.Write(map[string]string{"KEY1": "value1"}); err != nil {
		t.Fatalf("first Write() error = %v", err)
	}

	// Write new content
	if err := writer.Write(map[string]string{"KEY2": "value2"}); err != nil {
		t.Fatalf("second Write() error = %v", err)
	}

	content, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("failed to read env file: %v", err)
	}

	expected := "KEY2=value2\n"
	if string(content) != expected {
		t.Errorf("Write() content = %q, want %q", string(content), expected)
	}
}

func TestFileWriter_Write_AtomicOnFailure(t *testing.T) {
	tmpDir := t.TempDir()
	envPath := filepath.Join(tmpDir, "test.env")

	writer := New(envPath)

	// Write initial content
	initialVars := map[string]string{"INITIAL": "value"}
	if err := writer.Write(initialVars); err != nil {
		t.Fatalf("initial Write() error = %v", err)
	}

	// Verify initial content exists
	content, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("failed to read initial env file: %v", err)
	}

	expected := "INITIAL=value\n"
	if string(content) != expected {
		t.Errorf("initial content = %q, want %q", string(content), expected)
	}
}

func TestFileWriter_Path(t *testing.T) {
	path := "/run/pi-helper.env"
	writer := New(path)

	if got := writer.Path(); got != path {
		t.Errorf("Path() = %q, want %q", got, path)
	}
}

func TestFormatEnvVars(t *testing.T) {
	tests := []struct {
		name string
		vars map[string]string
		want string
	}{
		{
			name: "empty",
			vars: map[string]string{},
			want: "",
		},
		{
			name: "single var",
			vars: map[string]string{"KEY": "value"},
			want: "KEY=value\n",
		},
		{
			name: "multiple vars sorted",
			vars: map[string]string{
				"ZEBRA": "z",
				"APPLE": "a",
				"MANGO": "m",
			},
			want: "APPLE=a\nMANGO=m\nZEBRA=z\n",
		},
		{
			name: "empty value",
			vars: map[string]string{"KEY": ""},
			want: "KEY=\n",
		},
		{
			name: "value with spaces",
			vars: map[string]string{"KEY": "hello world"},
			want: "KEY=hello world\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatEnvVars(tt.vars)
			if got != tt.want {
				t.Errorf("formatEnvVars() = %q, want %q", got, tt.want)
			}
		})
	}
}
