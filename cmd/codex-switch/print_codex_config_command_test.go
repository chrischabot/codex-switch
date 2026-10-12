package main

import (
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/joshp123/codex-switch/internal/clientcapability"
)

func TestPrintCodexConfigCreatesStableSourceInstallConfiguration(t *testing.T) {
	dataDirectory := filepath.Join(t.TempDir(), "data")
	first := runPrintCodexConfigProcess(t, dataDirectory)
	capability, err := clientcapability.Read(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	want := "openai_base_url = \"http://127.0.0.1:2455/client/" + capability + "/backend-api/codex\"\n"
	if string(first) != want {
		t.Fatalf("first configuration = %q, want %q", first, want)
	}
	if second := runPrintCodexConfigProcess(t, dataDirectory); string(second) != want {
		t.Fatalf("second configuration = %q, want %q", second, want)
	}
	info, err := os.Stat(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("data directory mode = %o, want 0700", info.Mode().Perm())
	}
}

func TestPrintCodexConfigTargetsAnIsolatedLoopbackListener(t *testing.T) {
	dataDirectory := filepath.Join(t.TempDir(), "data")
	configuration := runPrintCodexConfigProcess(t, dataDirectory, "--listen", "127.0.0.1:52359")
	capability, err := clientcapability.Read(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	want := "openai_base_url = \"http://127.0.0.1:52359/client/" + capability + "/backend-api/codex\"\n"
	if string(configuration) != want {
		t.Fatalf("configuration = %q, want %q", configuration, want)
	}
}

func TestPrintCodexConfigProcess(t *testing.T) {
	if os.Getenv("CODEX_SWITCH_PRINT_CONFIG_PROCESS") != "1" {
		return
	}
	command := os.Getenv("CODEX_SWITCH_PRINT_CONFIG_COMMAND")
	if command == "" {
		command = "print-codex-config"
	}
	os.Args = append([]string{"codex-switch", command}, flag.Args()...)
	main()
	os.Exit(0)
}

func runPrintCodexConfigProcess(t *testing.T, dataDirectory string, args ...string) []byte {
	t.Helper()
	return runPrintConfigProcess(t, dataDirectory, "print-codex-config", args...)
}

func runPrintConfigProcess(t *testing.T, dataDirectory, commandName string, args ...string) []byte {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, append([]string{"-test.run=^TestPrintCodexConfigProcess$", "--"}, args...)...)
	command.Env = append(os.Environ(),
		"CODEX_SWITCH_PRINT_CONFIG_PROCESS=1",
		"CODEX_SWITCH_PRINT_CONFIG_COMMAND="+commandName,
		"CODEX_SWITCH_DATA_DIR="+dataDirectory,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("print Codex config: %v\n%s", err, output)
	}
	return output
}

func TestPrintedClaudeClientSettingsUseOnePrivatePoolAddress(t *testing.T) {
	dataDirectory := filepath.Join(t.TempDir(), "data")
	claudeSettings := runPrintConfigProcess(t, dataDirectory, "print-claude-config", "--listen", "127.0.0.1:52359")
	var settings struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(claudeSettings, &settings); err != nil {
		t.Fatal(err)
	}
	capability, err := clientcapability.Read(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	wantURL := "http://127.0.0.1:52359/client/" + capability + "/anthropic"
	if len(settings.Env) != 2 || settings.Env["HTTPS_PROXY"] != "http://codex-switch:"+capability+"@127.0.0.1:52359" || settings.Env["NODE_EXTRA_CA_CERTS"] != filepath.Join(dataDirectory, "claude-proxy-ca.pem") {
		t.Fatalf("Claude Code settings must contain only the forward proxy and its CA: %s", claudeSettings)
	}
	piSettings := runPrintConfigProcess(t, dataDirectory, "print-claude-config", "--pi", "--listen", "127.0.0.1:52359")
	var pi struct {
		Providers map[string]struct {
			BaseURL string `json:"baseUrl"`
			APIKey  string `json:"apiKey"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(piSettings, &pi); err != nil {
		t.Fatal(err)
	}
	provider := pi.Providers["anthropic"]
	if provider.BaseURL != wantURL || provider.APIKey != "sk-ant-oat01-codex-switch" {
		t.Fatalf("Pi settings use a different address or do not select the Claude subscription protocol: %s", piSettings)
	}
}
