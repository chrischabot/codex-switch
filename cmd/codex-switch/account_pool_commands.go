package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	accountapi "github.com/josh/codex-switch/internal/accounts"
	"github.com/josh/codex-switch/internal/codexstate"
	"github.com/josh/codex-switch/internal/fslock"
	"github.com/josh/codex-switch/internal/store"
)

// The login, accounts and remove commands hold pool.lock while they read or
// change the account pool, so two commands never change it at the same time.
// Accounts appear and disappear by directory rename, so readers without the
// lock, such as the proxy, see whole accounts only.

func loginCommand(args []string) {
	flags := flag.NewFlagSet("login", flag.ExitOnError)
	codex := flags.String("codex", codexstate.FindCodexExecutable(), "official Codex executable")
	_ = flags.Parse(args)
	if flags.NArg() > 1 || (flags.NArg() == 1 && !strings.HasPrefix(flags.Arg(0), "account-")) {
		log.Fatal("usage: codex-switch login [account-N]")
	}
	target := flags.Arg(0)
	if err := os.MkdirAll(dataDir(), 0o700); err != nil {
		log.Fatal(err)
	}
	staging, err := os.MkdirTemp(dataDir(), ".login-*")
	if err != nil {
		log.Fatal(err)
	}
	if target == "" {
		fmt.Println("Sign in to the account you want to add")
	} else {
		fmt.Printf("Sign in again to %s\n", target)
	}
	cmd := exec.Command(*codex, "login", "-c", `cli_auth_credentials_store="file"`)
	cmd.Env = append(os.Environ(), "CODEX_HOME="+staging)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		_ = trashCredentials(staging)
		log.Fatal(err)
	}
	added, err := accountapi.ReadPoolAccount(staging, "staged account", 0)
	if err != nil {
		_ = trashCredentials(staging)
		log.Fatal(err)
	}
	var published accountapi.PoolAccount
	err = fslock.With(filepath.Join(dataDir(), "pool.lock"), func() error {
		var publishErr error
		published, publishErr = publishLogin(dataDir(), staging, target, added)
		return publishErr
	})
	if err != nil {
		_ = trashCredentials(staging)
		log.Fatal(err)
	}
	if target != "" {
		if err := trashCredentials(staging); err != nil {
			log.Printf("warning: temporary login files remain at %s: %v", staging, err)
		}
		fmt.Printf("Updated %s  %s\n", published.Name, published.Email)
		return
	}
	fmt.Printf("Added %s  %s\n", published.Name, published.Email)
}

func publishLogin(root, staging, target string, added accountapi.PoolAccount) (accountapi.PoolAccount, error) {
	existing, err := accountapi.ReadPool(root)
	if err != nil {
		return accountapi.PoolAccount{}, err
	}
	if target != "" {
		var selected *accountapi.PoolAccount
		for index := range existing {
			if existing[index].Name == target {
				selected = &existing[index]
				break
			}
		}
		if selected == nil {
			return accountapi.PoolAccount{}, fmt.Errorf("unknown account %s", target)
		}
		if added.ID != selected.ID {
			return accountapi.PoolAccount{}, fmt.Errorf("the browser signed in to a different ChatGPT account; %s was not changed", target)
		}
		active := filepath.Join(root, target)
		if err := fslock.With(filepath.Join(active, "auth.lock"), func() error {
			return replaceAuthFile(active, filepath.Join(staging, "auth.json"))
		}); err != nil {
			return accountapi.PoolAccount{}, err
		}
		added.Name = selected.Name
		added.Index = selected.Index
		return added, nil
	}

	for _, account := range existing {
		if account.ID == added.ID {
			return accountapi.PoolAccount{}, fmt.Errorf("the same ChatGPT account was selected more than once")
		}
	}
	database, err := store.OpenStoreDatabase(filepath.Join(root, "codex-switch.db"))
	if err != nil {
		return accountapi.PoolAccount{}, err
	}
	defer database.CloseStoreDatabase()
	for _, account := range existing {
		if err := rememberPoolAccount(database, account); err != nil {
			return accountapi.PoolAccount{}, err
		}
	}
	index, err := database.NextAccountNumber()
	if err != nil {
		return accountapi.PoolAccount{}, err
	}
	name := fmt.Sprintf("account-%d", index)
	added.Name = name
	if err := rememberPoolAccount(database, added); err != nil {
		return accountapi.PoolAccount{}, err
	}
	if err := os.Rename(staging, filepath.Join(root, name)); err != nil {
		return accountapi.PoolAccount{}, err
	}
	added.Name = name
	added.Index = index
	return added, nil
}

func replaceAuthFile(activeDir, stagedPath string) error {
	if err := replaceAuthFileWithRename(activeDir, stagedPath, os.Rename); err != nil {
		return err
	}
	directory, err := os.Open(activeDir)
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

func replaceAuthFileWithRename(activeDir, stagedPath string, rename func(string, string) error) error {
	data, err := os.ReadFile(stagedPath)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(activeDir, ".auth-*")
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
	if err := rename(temporaryPath, filepath.Join(activeDir, "auth.json")); err != nil {
		return err
	}
	return nil
}

func accountsCommand(args []string) {
	if len(args) != 0 {
		log.Fatal("usage: codex-switch accounts")
	}
	var values []accountapi.PoolAccount
	err := fslock.With(filepath.Join(dataDir(), "pool.lock"), func() error {
		var err error
		values, err = accountapi.ReadPool(dataDir())
		return err
	})
	if err != nil {
		log.Fatal(err)
	}
	if len(values) == 0 {
		fmt.Println("No accounts. Run codex-switch login.")
		return
	}
	for _, account := range values {
		fmt.Printf("%s\t%s\n", account.Name, account.Email)
	}
}

func removeCommand(args []string) {
	if len(args) != 1 || !strings.HasPrefix(args[0], "account-") {
		log.Fatal("usage: codex-switch remove account-N")
	}
	var selected *accountapi.PoolAccount
	var removed string
	var removedPins int64
	err := fslock.With(filepath.Join(dataDir(), "pool.lock"), func() error {
		values, err := accountapi.ReadPool(dataDir())
		if err != nil {
			return err
		}
		for index := range values {
			if values[index].Name == args[0] {
				selected = &values[index]
				break
			}
		}
		if selected == nil {
			return fmt.Errorf("unknown account %s", args[0])
		}
		removed = filepath.Join(dataDir(), fmt.Sprintf(".remove-%s-%d", selected.Name, time.Now().Unix()))
		active := filepath.Join(dataDir(), selected.Name)
		if err := fslock.WithExistingDirectory(filepath.Join(active, "auth.lock"), func() error {
			return os.Rename(active, removed)
		}); err != nil {
			return err
		}
		storeDatabase, err := store.OpenStoreDatabase(filepath.Join(dataDir(), "codex-switch.db"))
		if err != nil {
			return errors.Join(err, restoreAccountAfterFailedRemoval(removed, active))
		}
		for _, account := range values {
			if err := rememberPoolAccount(storeDatabase, account); err != nil {
				storeDatabase.CloseStoreDatabase()
				return errors.Join(err, restoreAccountAfterFailedRemoval(removed, active))
			}
		}
		removedPins, err = storeDatabase.RemoveAccountRoutingState(store.AccountName(selected.Name))
		closeErr := storeDatabase.CloseStoreDatabase()
		if err != nil || closeErr != nil {
			return errors.Join(err, closeErr, restoreAccountAfterFailedRemoval(removed, active))
		}
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := trashCredentials(removed); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Removed %s (%s) and %d thread pins.\n", selected.Name, selected.Email, removedPins)
}

func rememberPoolAccount(database *store.StoreDatabase, account accountapi.PoolAccount) error {
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

func trashCredentials(home string) error {
	return exec.Command("trash", home).Run()
}
