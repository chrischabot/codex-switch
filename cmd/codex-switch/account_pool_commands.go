package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	accountapi "github.com/josh/codex-switch/internal/accounts"
	"github.com/josh/codex-switch/internal/codexstate"
)

func loginCommand(args []string) {
	flags := flag.NewFlagSet("login", flag.ExitOnError)
	codex := flags.String("codex", codexstate.FindCodexExecutable(), "official Codex executable")
	_ = flags.Parse(args)
	if flags.NArg() > 1 || (flags.NArg() == 1 && !strings.HasPrefix(flags.Arg(0), "account-")) {
		log.Fatal("usage: codex-switch login [account-N]")
	}
	target := flags.Arg(0)
	if target == "" {
		fmt.Println("Sign in to the account you want to add")
	} else {
		fmt.Printf("Sign in again to %s\n", target)
	}
	published, err := accountapi.SignIn(dataDir(), *codex, target)
	if err != nil {
		log.Fatal(err)
	}
	if target != "" {
		fmt.Printf("Updated %s  %s\n", published.Name, published.Email)
		return
	}
	fmt.Printf("Added %s  %s\n", published.Name, published.Email)
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
		fmt.Printf("%s\t%s\n", account.Name, account.Email)
	}
}

func removeCommand(args []string) {
	if len(args) != 1 || !strings.HasPrefix(args[0], "account-") {
		log.Fatal("usage: codex-switch remove account-N")
	}
	removed, removedPins, err := accountapi.RemoveAccount(dataDir(), args[0])
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Removed %s (%s) and %d thread pins.\n", removed.Name, removed.Email, removedPins)
}
