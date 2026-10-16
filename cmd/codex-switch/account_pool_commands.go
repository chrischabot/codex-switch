package main

import (
	"flag"
	"fmt"
	"log"
	"strconv"

	accountapi "github.com/joshp123/codex-switch/internal/accounts"
	"github.com/joshp123/codex-switch/internal/codexstate"
)

func loginCommand(args []string) {
	flags := flag.NewFlagSet("login", flag.ExitOnError)
	codex := flags.String("codex", codexstate.FindCodexExecutable(), "official Codex executable")
	claude := flags.Bool("claude", false, "sign in to a Claude subscription account")
	_ = flags.Parse(args)
	if flags.NArg() > 1 {
		log.Fatal("usage: codex-switch login [--claude] [account-N | \"Codex N\" | \"Claude N\"]")
	}
	target := flags.Arg(0)
	if target == "" {
		fmt.Println("Sign in to the account you want to add")
	} else {
		pool, err := accountapi.ReadPool(dataDir())
		if err != nil {
			log.Fatal(err)
		}
		found := false
		for _, account := range pool {
			if target == account.Name || target == account.DisplayName {
				target = account.Name
				fmt.Printf("Sign in again to %s\n", account.DisplayName)
				found = true
				break
			}
		}
		if !found {
			log.Fatalf("unknown account %s", target)
		}
	}
	var published accountapi.PoolAccount
	var err error
	if *claude {
		published, err = accountapi.SignInClaude(dataDir(), target)
	} else {
		published, err = accountapi.SignIn(dataDir(), *codex, target)
	}
	if err != nil {
		log.Fatal(err)
	}
	pool, err := accountapi.ReadPool(dataDir())
	if err != nil {
		log.Fatal(err)
	}
	displayName, _ := accountapi.AccountDisplayName(pool, published.Name)
	if target != "" {
		fmt.Printf("Updated %s  %s\n", displayName, published.Email)
		return
	}
	fmt.Printf("Added %s  %s\n", displayName, published.Email)
}

func accountsCommand(args []string) {
	if len(args) != 0 {
		log.Fatal("usage: codex-switch accounts")
	}
	pool, err := accountapi.ReadPool(dataDir())
	if err != nil {
		log.Fatal(err)
	}
	if len(pool) == 0 {
		fmt.Println("No accounts. Run codex-switch login.")
		return
	}
	for _, account := range pool {
		fmt.Printf("%s\t%s\t%s\n", account.DisplayName, account.Name, account.Email)
	}
}

func removeCommand(args []string) {
	if len(args) != 1 {
		log.Fatal("usage: codex-switch remove account-N | \"Codex N\" | \"Claude N\"")
	}
	removed, removedPins, err := accountapi.RemoveAccount(dataDir(), args[0])
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Removed %s (%s) and %d thread pins.\n", removed.DisplayName, removed.Email, removedPins)
}

func reserveCommand(args []string) {
	if len(args) != 2 {
		log.Fatal("usage: codex-switch reserve account-N | \"Codex N\" | \"Claude N\" percent | off")
	}
	percent := 0
	if args[1] != "off" {
		var err error
		percent, err = strconv.Atoi(args[1])
		if err != nil {
			log.Fatal("reserve must be a whole percentage from 0 to 100, or off")
		}
	}
	account, err := accountapi.SetReservePercent(dataDir(), args[0], percent)
	if err != nil {
		log.Fatal(err)
	}
	if percent == 0 {
		fmt.Printf("%s: reserve off\n", account.DisplayName)
	} else {
		fmt.Printf("%s: %d%% kept for direct use\n", account.DisplayName, percent)
	}
}
