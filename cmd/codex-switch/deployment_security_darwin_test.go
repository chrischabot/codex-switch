package main

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"golang.org/x/sys/unix"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise just the read-only status function, never the installation script.
func TestDeploymentStatusKeepsCapabilityOutOfArguments(t *testing.T) {
	for _, item := range []struct{ file, name string }{{"deploy.sh", "deploy_status"}, {"install-proxy.sh", "serving_status"}} {
		t.Run(item.file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("../../deploy", item.file))
			if err != nil {
				t.Fatal(err)
			}
			source := string(raw)
			start := strings.Index(source, item.name+"() {")
			if start < 0 {
				t.Fatal("missing status function")
			}
			tail := source[start:]
			end := strings.Index(tail, "}")
			function := tail[:end+1]
			b := make([]byte, 32)
			rand.Read(b)
			capability := hex.EncodeToString(b)
			data := t.TempDir()
			os.WriteFile(filepath.Join(data, "client-capability"), []byte(capability), 0600)
			entered, release := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; w.Write([]byte("{}")) }))
			defer server.Close()
			cmd := exec.CommandContext(t.Context(), "/bin/sh", "-c", function+"\n"+item.name)
			cmd.Env = append(os.Environ(), "DATA="+data, "data="+data, "listen="+strings.TrimPrefix(server.URL, "http://"), "STATUS_URL="+server.URL+"/client/"+capability+"/codex-switch/deploy-status")
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { close(release); cmd.Wait() }()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("status request did not arrive")
			}
			assertChildArgumentsPrivate(t, cmd.Process.Pid, capability)

		})
	}
}

func TestUninstallRefusesClaudeProxyConfiguration(t *testing.T) {
	raw, err := os.ReadFile("../../deploy/uninstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "# Codex")
	end := strings.Index(source, "/bin/launchctl")
	if start < 0 || end < start {
		t.Fatal("cannot isolate uninstall guard")
	}
	home := t.TempDir()
	data := filepath.Join(home, "data")
	os.Mkdir(data, 0700)
	os.Mkdir(filepath.Join(home, ".claude"), 0700)
	os.WriteFile(filepath.Join(data, "client-capability"), []byte("synthetic-capability"), 0600)
	os.WriteFile(filepath.Join(home, ".claude/settings.json"), []byte(`{"env":{"HTTPS_PROXY":"http://codex-switch:synthetic-capability@127.0.0.1:2455"}}`), 0600)
	cmd := exec.Command("/bin/sh", "-c", source[start:end]+"\nexit 0\n")
	cmd.Env = append(os.Environ(), "HOME="+home, "data="+data)
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("uninstall accepted active Claude settings: %s", output)
	}
}

// Inspect only fixture descendants, never unrelated applications or environments.
func assertChildArgumentsPrivate(t *testing.T, parent int, secret string) {
	t.Helper()
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		t.Fatal(err)
	}
	children := map[int32]bool{int32(parent): true}
	for range 4 {
		for _, p := range processes {
			if children[p.Eproc.Ppid] {
				children[p.Proc.P_pid] = true
			}
		}
	}
	inspected := 0
	for pid := range children {
		raw, err := unix.SysctlRaw("kern.procargs2", int(pid))
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) < 4 {
			t.Fatal("missing argument count")
		}
		argc := int(binary.LittleEndian.Uint32(raw[:4]))
		raw = raw[4:]
		end := bytes.IndexByte(raw, 0)
		if end < 0 {
			t.Fatal("missing executable")
		}
		raw = bytes.TrimLeft(raw[end:], "\x00")
		for range argc {
			end = bytes.IndexByte(raw, 0)
			if end < 0 {
				t.Fatal("missing argument")
			}
			if bytes.Contains(raw[:end], []byte(secret)) {
				t.Fatal("capability exposed in process arguments")
			}
			raw = raw[end+1:]
		}
		inspected++
	}
	if inspected < 2 {
		t.Fatal("no curl child was inspected")
	}
}

func TestUninstallRequiresAcknowledgementForPrivateDesktopOverrides(t *testing.T) {
	raw, err := os.ReadFile("../../deploy/uninstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "# Codex")
	end := strings.Index(source, "/bin/launchctl")
	home := t.TempDir()
	data := filepath.Join(home, "data")
	os.Mkdir(data, 0700)
	os.WriteFile(filepath.Join(data, "claude-proxy-ca.pem"), []byte("synthetic public certificate"), 0600)
	for _, ack := range []string{"0", "1"} {
		cmd := exec.Command("/bin/sh", "-c", source[start:end]+"\nexit 0\n")
		cmd.Env = append(os.Environ(), "HOME="+home, "data="+data, "CODEX_SWITCH_CLAUDE_DISCONNECTED="+ack)
		out, err := cmd.CombinedOutput()
		if (err == nil) != (ack == "1") {
			t.Fatalf("ack=%s result=%v %s", ack, err, out)
		}
	}
}
