package accounts

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCodexClientVersionExecutableContract(t *testing.T) {
	for _, test := range []struct {
		name, output, want string
		wantError          bool
	}{
		{"current client", "codex-cli 0.159.2", "0.159.2", false},
		{"prerelease client", "codex-cli 0.160.0-alpha.1", "0.160.0-alpha.1", false},
		{"not Codex", "other-cli 0.159.2", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			executable := filepath.Join(t.TempDir(), "codex")
			if err := os.WriteFile(executable, []byte("#!/bin/sh\n[ \"$#\" = 1 ] && [ \"$1\" = --version ] || exit 1\nprintf '"+test.output+"\\n'\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			version, err := codexClientVersion(context.Background(), executable)
			if (err != nil) != test.wantError || version != test.want {
				t.Fatalf("version = %q, error = %v; want %q", version, err, test.want)
			}
		})
	}
}

func writeCodexExecutableVersion(t *testing.T, executable, version string) {
	t.Helper()
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n[ \"$#\" = 1 ] && [ \"$1\" = --version ] || exit 1\nprintf 'codex-cli "+version+"\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}
