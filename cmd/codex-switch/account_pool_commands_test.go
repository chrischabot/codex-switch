package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	accountapi "github.com/josh/codex-switch/internal/accounts"
)

func TestPublishLoginReplacesOnlyTheNamedMatchingAccount(t *testing.T) {
	root := t.TempDir()
	writeAccountFixture(t, filepath.Join(root, "account-1"), "user-one", "one@example.com", "old-access")
	writeAccountFixture(t, filepath.Join(root, "account-2"), "user-two", "two@example.com", "other-access")
	staging := filepath.Join(root, ".login-staging")
	added := writeAccountFixture(t, staging, "user-one", "one@example.com", "new-access")

	published, err := publishLogin(root, staging, "account-1", added)
	if err != nil {
		t.Fatal(err)
	}
	if published.Name != "account-1" || published.Index != 1 {
		t.Fatalf("published = %#v", published)
	}
	if got := accountAccessToken(t, filepath.Join(root, "account-1")); got != "new-access" {
		t.Fatalf("active access token = %q", got)
	}
	if got := accountAccessToken(t, staging); got != "new-access" {
		t.Fatalf("staged access token = %q", got)
	}
	if got := accountAccessToken(t, filepath.Join(root, "account-2")); got != "other-access" {
		t.Fatalf("unrelated access token = %q", got)
	}
}

func TestPublishLoginRejectsInvalidNamedTargetsWithoutChangingCredentials(t *testing.T) {
	for _, test := range []struct {
		name        string
		target      string
		stagedUser  string
		stagedEmail string
		wantError   string
	}{
		{
			name: "wrong identity", target: "account-1",
			stagedUser: "user-two", stagedEmail: "two@example.com",
			wantError: "different ChatGPT account",
		},
		{
			name: "unknown account", target: "account-2",
			stagedUser: "user-one", stagedEmail: "one@example.com",
			wantError: "unknown account account-2",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			writeAccountFixture(t, filepath.Join(root, "account-1"), "user-one", "one@example.com", "old-access")
			staging := filepath.Join(root, ".login-staging")
			added := writeAccountFixture(t, staging, test.stagedUser, test.stagedEmail, "new-access")

			if _, err := publishLogin(root, staging, test.target, added); err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v", err)
			}
			if got := accountAccessToken(t, filepath.Join(root, "account-1")); got != "old-access" {
				t.Fatalf("active access token = %q", got)
			}
			if got := accountAccessToken(t, staging); got != "new-access" {
				t.Fatalf("staged access token = %q", got)
			}
		})
	}
}

func TestPublishLoginAddsSixthAndTwentiethAccounts(t *testing.T) {
	for _, count := range []int{6, 20} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			root := t.TempDir()
			for index := 1; index < count; index++ {
				writeAccountFixture(t, filepath.Join(root, fmt.Sprintf("account-%d", index)), fmt.Sprintf("user-%d", index), fmt.Sprintf("user-%d@example.com", index), "old-access")
			}
			staging := filepath.Join(root, ".login-staging")
			added := writeAccountFixture(t, staging, fmt.Sprintf("user-%d", count), fmt.Sprintf("user-%d@example.com", count), "new-access")

			published, err := publishLogin(root, staging, "", added)
			if err != nil {
				t.Fatal(err)
			}
			wantName := fmt.Sprintf("account-%d", count)
			if published.Name != wantName || published.Index != count {
				t.Fatalf("published = %#v", published)
			}
			if got := accountAccessToken(t, filepath.Join(root, wantName)); got != "new-access" {
				t.Fatalf("published access token = %q", got)
			}
			if _, err := os.Stat(staging); !os.IsNotExist(err) {
				t.Fatalf("staging directory remains: %v", err)
			}
		})
	}
}

func TestAtomicAuthReplacementFailureLeavesTheActiveCredentialReadable(t *testing.T) {
	root := t.TempDir()
	active := filepath.Join(root, "account-1")
	staging := filepath.Join(root, ".login-staging")
	writeAccountFixture(t, active, "user-one", "one@example.com", "old-access")
	writeAccountFixture(t, staging, "user-one", "one@example.com", "new-access")

	errInjected := errors.New("injected rename failure")
	err := replaceAuthFileWithRename(active, filepath.Join(staging, "auth.json"), func(_, _ string) error {
		return errInjected
	})
	if !errors.Is(err, errInjected) {
		t.Fatalf("error = %v", err)
	}
	if got := accountAccessToken(t, active); got != "old-access" {
		t.Fatalf("active access token = %q", got)
	}
	if got := accountAccessToken(t, staging); got != "new-access" {
		t.Fatalf("staged access token = %q", got)
	}
	entries, err := os.ReadDir(active)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "auth.json" {
		t.Fatalf("active directory = %#v", entries)
	}
}

func writeAccountFixture(t *testing.T, home, userID, email, accessToken string) accountapi.PoolAccount {
	t.Helper()
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	header, _ := json.Marshal(map[string]string{"alg": "none"})
	payload, _ := json.Marshal(map[string]any{
		"email":                       email,
		"https://api.openai.com/auth": map[string]string{"chatgpt_user_id": userID},
	})
	idToken := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload) + ".x"
	data, _ := json.Marshal(map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]string{
			"account_id": "subscription-" + userID, "id_token": idToken,
			"access_token": accessToken, "refresh_token": "refresh-" + userID,
		},
	})
	if err := os.WriteFile(filepath.Join(home, "auth.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	account, err := accountapi.ReadPoolAccount(home, filepath.Base(home), 0)
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func accountAccessToken(t *testing.T, home string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value.Tokens.AccessToken
}

func TestLoginRepairsTheNamedAccount(t *testing.T) {
	root := t.TempDir()
	writeAccountFixture(t, filepath.Join(root, "account-1"), "user-one", "one@example.com", "old-access")
	writeAccountFixture(t, filepath.Join(root, "account-2"), "user-two", "two@example.com", "other-access")
	fakeRoot := t.TempDir()
	staged := filepath.Join(fakeRoot, "staged")
	writeAccountFixture(t, staged, "user-one", "one@example.com", "new-access")
	fakeCodex := filepath.Join(fakeRoot, "codex")
	script := `#!/bin/sh
set -eu
test "$#" -eq 3
test "$1" = "login"
test "$2" = "-c"
test "$3" = 'cli_auth_credentials_store="file"'
cp "$CODEX_SWITCH_FAKE_AUTH" "$CODEX_HOME/auth.json"
`
	if err := os.WriteFile(fakeCodex, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_SWITCH_DATA_DIR", root)
	t.Setenv("CODEX_SWITCH_FAKE_CODEX", fakeCodex)
	t.Setenv("CODEX_SWITCH_FAKE_AUTH", filepath.Join(staged, "auth.json"))

	output, err := runLoginCLIProcess(t, "account-1")
	if err != nil {
		t.Fatalf("named login while runtime active: %v\n%s", err, output)
	}
	if got := accountAccessToken(t, filepath.Join(root, "account-1")); got != "new-access" {
		t.Fatalf("account-1 access token = %q", got)
	}
	if got := accountAccessToken(t, filepath.Join(root, "account-2")); got != "other-access" {
		t.Fatalf("account-2 access token = %q", got)
	}
}

func TestLoginCLIProcess(t *testing.T) {
	if os.Getenv("CODEX_SWITCH_LOGIN_CLI_PROCESS") != "1" {
		return
	}
	os.Args = []string{
		"codex-switch", "login", "--codex", os.Getenv("CODEX_SWITCH_FAKE_CODEX"),
	}
	if target := os.Getenv("CODEX_SWITCH_LOGIN_TARGET"); target != "" {
		os.Args = append(os.Args, target)
	}
	main()
}

func runLoginCLIProcess(t *testing.T, target string) ([]byte, error) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestLoginCLIProcess$")
	command.Env = append(os.Environ(),
		"CODEX_SWITCH_LOGIN_CLI_PROCESS=1",
		"CODEX_SWITCH_LOGIN_TARGET="+target,
	)
	return command.CombinedOutput()
}
