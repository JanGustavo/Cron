package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadReadsEnvFromParentDirectory(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		root := os.Getenv("CRONFLOW_TEST_ROOT")
		subdir := filepath.Join(root, "cmd", "check-ai")
		if err := os.Chdir(subdir); err != nil {
			t.Fatalf("chdir to %s: %v", subdir, err)
		}
		os.Unsetenv("DATABASE_URL")

		cfg := Load()
		if cfg.DatabaseURL != "postgresql://cronflow:cronflow_secret@localhost:5432/cronflow?sslmode=disable" {
			os.Exit(2)
		}
		return
	}

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "cmd", "check-ai"), 0o755); err != nil {
		t.Fatalf("mkdir structure: %v", err)
	}

	content := "DATABASE_URL=postgresql://cronflow:cronflow_secret@localhost:5432/cronflow?sslmode=disable\n"
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(content), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestLoadReadsEnvFromParentDirectory")
	cmd.Dir = filepath.Join(root, "cmd", "check-ai")
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "CRONFLOW_TEST_ROOT="+root)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("expected parent .env to be read from nested working directory, got error: %v\n%s", err, string(out))
	}
	if strings.Contains(string(out), "FATAL: variável de ambiente obrigatória não definida DATABASE_URL") {
		t.Fatalf("expected DATABASE_URL to load from parent .env, but output included fatal error: %s", out)
	}
}
