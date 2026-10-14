package accounts

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/joshp123/codex-switch/internal/store"
)

// SignIn runs Codex's browser login into a staging directory, then adds the
// signed-in account to the pool. With accountName set, it replaces that
// account's login instead, and only if the browser signed in to the same user.
func SignIn(dataDirectory, codexExecutable, accountName string) (PoolAccount, error) {
	if err := os.MkdirAll(dataDirectory, 0o700); err != nil {
		return PoolAccount{}, err
	}
	staging, err := os.MkdirTemp(dataDirectory, ".login-*")
	if err != nil {
		return PoolAccount{}, err
	}
	login := exec.Command(codexExecutable, "login", "-c", `cli_auth_credentials_store="file"`)
	login.Env = append(os.Environ(), "CODEX_HOME="+staging)
	login.Stdin, login.Stdout, login.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := login.Run(); err != nil {
		_ = trash(staging)
		return PoolAccount{}, err
	}
	signedIn, err := ReadPoolAccount(staging, "staged account", 0)
	if err != nil {
		_ = trash(staging)
		return PoolAccount{}, err
	}
	var published PoolAccount
	err = withPoolLock(dataDirectory, func() error {
		var publishErr error
		published, publishErr = publishLogin(dataDirectory, staging, accountName, signedIn)
		return publishErr
	})
	if err != nil {
		_ = trash(staging)
		return PoolAccount{}, err
	}
	if accountName != "" {
		if err := trash(staging); err != nil {
			log.Printf("warning: temporary login files remain at %s: %v", staging, err)
		}
	}
	return published, nil
}

func publishLogin(dataDirectory, staging, accountName string, signedIn PoolAccount) (PoolAccount, error) {
	existing, err := ReadPool(dataDirectory)
	if err != nil {
		return PoolAccount{}, err
	}
	if accountName != "" {
		var selected *PoolAccount
		for index := range existing {
			if existing[index].Name == accountName {
				selected = &existing[index]
				break
			}
		}
		if selected == nil {
			return PoolAccount{}, fmt.Errorf("unknown account %s", accountName)
		}
		if signedIn.Provider != selected.Provider || signedIn.ID != selected.ID {
			providerName := "ChatGPT"
			if signedIn.Provider == ProviderClaude {
				providerName = "Claude"
			}
			return PoolAccount{}, fmt.Errorf("the browser signed in to a different %s account; %s was not changed", providerName, selected.DisplayName)
		}
		active := filepath.Join(dataDirectory, accountName)
		if err := withAuthLock(active, func() error {
			return replaceAuthFile(active, filepath.Join(staging, "auth.json"))
		}); err != nil {
			return PoolAccount{}, err
		}
		signedIn.Name = selected.Name
		signedIn.Index = selected.Index
		return signedIn, nil
	}

	for _, account := range existing {
		if account.Provider == signedIn.Provider && account.ID == signedIn.ID {
			providerName := "ChatGPT"
			if signedIn.Provider == ProviderClaude {
				providerName = "Claude"
			}
			return PoolAccount{}, fmt.Errorf("the same %s account was selected more than once", providerName)
		}
	}
	database, err := store.OpenStoreDatabase(filepath.Join(dataDirectory, "codex-switch.db"))
	if err != nil {
		return PoolAccount{}, err
	}
	defer database.CloseStoreDatabase()
	for _, account := range existing {
		if err := rememberPoolAccount(database, account); err != nil {
			return PoolAccount{}, err
		}
	}
	index, err := database.NextAccountNumber()
	if err != nil {
		return PoolAccount{}, err
	}
	name := fmt.Sprintf("account-%d", index)
	signedIn.Name = name
	if err := rememberPoolAccount(database, signedIn); err != nil {
		return PoolAccount{}, err
	}
	if err := os.Rename(staging, filepath.Join(dataDirectory, name)); err != nil {
		return PoolAccount{}, err
	}
	signedIn.Index = index
	return signedIn, nil
}

func replaceAuthFile(accountDirectory, stagedPath string) error {
	if err := replaceAuthFileWithRename(accountDirectory, stagedPath, os.Rename); err != nil {
		return err
	}
	directory, err := os.Open(accountDirectory)
	if err != nil {
		log.Printf("warning: credentials were updated but the account directory could not be synced: %v", err)
		return nil
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		log.Printf("warning: credentials were updated but the account directory could not be synced: %v", err)
	}
	return nil
}

func replaceAuthFileWithRename(accountDirectory, stagedPath string, rename func(string, string) error) error {
	data, err := os.ReadFile(stagedPath)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(accountDirectory, ".auth-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return rename(temporaryPath, filepath.Join(accountDirectory, "auth.json"))
}

// RemoveAccount takes the account out of the pool, forgets its task pins and
// moves its saved login to the Trash. It returns the removed account and how
// many task pins it had.
func RemoveAccount(dataDirectory, accountName string) (PoolAccount, int64, error) {
	var selected *PoolAccount
	var removed string
	var removedPins int64
	err := withPoolLock(dataDirectory, func() error {
		pool, err := ReadPool(dataDirectory)
		if err != nil {
			return err
		}
		for index := range pool {
			if pool[index].Name == accountName || pool[index].DisplayName == accountName {
				selected = &pool[index]
				break
			}
		}
		if selected == nil {
			return fmt.Errorf("unknown account %s", accountName)
		}
		removed = filepath.Join(dataDirectory, fmt.Sprintf(".remove-%s-%d", selected.Name, time.Now().Unix()))
		active := filepath.Join(dataDirectory, selected.Name)
		if err := withAuthLock(active, func() error {
			return os.Rename(active, removed)
		}); err != nil {
			return err
		}
		database, err := store.OpenStoreDatabase(filepath.Join(dataDirectory, "codex-switch.db"))
		if err != nil {
			return errors.Join(err, restoreAccountAfterFailedRemoval(removed, active))
		}
		for _, account := range pool {
			if err := rememberPoolAccount(database, account); err != nil {
				database.CloseStoreDatabase()
				return errors.Join(err, restoreAccountAfterFailedRemoval(removed, active))
			}
		}
		removedPins, err = database.RemoveAccountRoutingState(store.AccountName(selected.Name))
		closeErr := database.CloseStoreDatabase()
		if err != nil || closeErr != nil {
			return errors.Join(err, closeErr, restoreAccountAfterFailedRemoval(removed, active))
		}
		return nil
	})
	if err != nil {
		return PoolAccount{}, 0, err
	}
	if err := trash(removed); err != nil {
		return PoolAccount{}, 0, err
	}
	return *selected, removedPins, nil
}

func rememberPoolAccount(database *store.StoreDatabase, account PoolAccount) error {
	return database.RememberAccountIdentity(store.AccountIdentity{
		AccountName: store.AccountName(account.Name), ProviderAccountID: account.AccountID, Email: account.Email,
	})
}

func restoreAccountAfterFailedRemoval(removedPath, activePath string) error {
	if err := os.Rename(removedPath, activePath); err != nil {
		return fmt.Errorf("restore account after failed removal: %w", err)
	}
	return nil
}

// trash moves saved credentials to the macOS Trash rather than deleting them.
func trash(path string) error {
	return exec.Command("trash", path).Run()
}
