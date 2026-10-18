package accounts

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Provider string

const (
	ProviderCodex   Provider = ""
	ProviderChatGPT Provider = ProviderCodex
	ProviderClaude  Provider = "claude"
)

// PoolAccount is one signed-in subscription account. Pool accounts live in
// account-N directories in the data directory, each holding an auth.json.
type PoolAccount struct {
	ReadError      error // Invalid entries remain visible and removable, but never route work.
	Provider       Provider
	OrganizationID string
	Plan           string
	// Name is the directory name, such as account-3, and Index its number.
	Name           string
	Index          int
	DisplayName    string
	ProviderNumber int
	Reserve        AccountReserve
	// ID identifies the subscription user within its provider.
	ID string
	// AccountID is the immutable subscription identity. For ChatGPT it is sent
	// as ChatGPT-Account-ID; for Claude it joins the account and organization IDs.
	AccountID string
	Email     string
}

// ReadPool returns every signed-in account in dataDirectory, ordered by
// account number. A missing data directory is an empty pool.
func ReadPool(dataDirectory string) ([]PoolAccount, error) {
	entries, err := os.ReadDir(dataDirectory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var result []PoolAccount
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "account-") {
			continue
		}
		index, err := strconv.Atoi(strings.TrimPrefix(entry.Name(), "account-"))
		if err != nil || index < 1 {
			continue
		}
		account, err := ReadPoolAccount(filepath.Join(dataDirectory, entry.Name()), entry.Name(), index)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			account = PoolAccount{Name: entry.Name(), Index: index, ReadError: fmt.Errorf("invalid saved account; remove or restore its auth file")}
		}
		account.Reserve, err = readAccountReserve(filepath.Join(dataDirectory, entry.Name()))
		if err != nil {
			account.ReadError = fmt.Errorf("invalid reserve configuration; restore its reserve file")
		}
		result = append(result, account)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Index < result[j].Index })
	for index := range result {
		result[index].DisplayName, result[index].ProviderNumber = AccountDisplayName(result, result[index].Name)
	}
	return result, nil
}

// ReadPoolAccount reads the provider identity from the auth.json in
// accountDirectory.
func ReadPoolAccount(accountDirectory, name string, index int) (PoolAccount, error) {
	data, err := os.ReadFile(filepath.Join(accountDirectory, "auth.json"))
	if err != nil {
		return PoolAccount{}, err
	}
	var value struct {
		Provider Provider `json:"provider"`
		Tokens   struct {
			AccountID string `json:"account_id"`
			IDToken   string `json:"id_token"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return PoolAccount{}, err
	}
	if value.Provider == ProviderClaude {
		return readClaudePoolAccount(data, name, index)
	}
	if value.Tokens.AccountID == "" {
		return PoolAccount{}, fmt.Errorf("%s has no ChatGPT account ID", name)
	}
	email, userID, err := decodeIDTokenIdentity(value.Tokens.IDToken)
	if err != nil {
		return PoolAccount{}, fmt.Errorf("read %s identity: %w", name, err)
	}
	identity := userID
	if identity == "" {
		identity = value.Tokens.AccountID + "|" + email
	}
	return PoolAccount{
		Name: name, Index: index, ID: identity,
		AccountID: value.Tokens.AccountID, Email: email,
	}, nil
}

func decodeIDTokenIdentity(token string) (string, string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", "", errors.New("ID token is not a JWT")
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", fmt.Errorf("decode ID token claims: %w", err)
	}
	var claims struct {
		Email string `json:"email"`
		Auth  struct {
			ChatGPTUserID string `json:"chatgpt_user_id"`
			UserID        string `json:"user_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(data, &claims); err != nil {
		return "", "", fmt.Errorf("decode ID token identity: %w", err)
	}
	if claims.Email == "" {
		return "", "", errors.New("ID token has no email")
	}
	userID := claims.Auth.ChatGPTUserID
	if userID == "" {
		userID = claims.Auth.UserID
	}
	return claims.Email, userID, nil
}

// AccountDisplayName derives a name and provider-local number from pool order.
// A key outside the current pool belongs to a removed account.
func AccountDisplayName(pool []PoolAccount, key string) (string, int) {
	numbers := map[Provider]int{}
	for _, account := range pool {
		numbers[account.Provider]++
		if account.Name == key {
			provider := "Codex"
			if account.Provider == ProviderClaude {
				provider = "Claude"
			}
			number := numbers[account.Provider]
			return fmt.Sprintf("%s %d", provider, number), number
		}
	}
	return "removed account", 0
}
