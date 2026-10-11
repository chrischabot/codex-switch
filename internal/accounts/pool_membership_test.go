package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
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
