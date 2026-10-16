package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"
)

// startDetachedCommand keeps the deployment bridge alive after its caller exits.
func startDetachedCommand(args []string) {
	if len(args) < 2 {
		log.Fatal("usage: codex-switch start-detached <log-file> <executable> [arguments...]")
	}
	output, err := os.OpenFile(args[0], os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		log.Fatal(err)
	}
	defer output.Close()
	input, err := os.Open(os.DevNull)
	if err != nil {
		log.Fatal(err)
	}
	defer input.Close()
	command := exec.Command(args[1], args[2:]...)
	command.Stdin, command.Stdout, command.Stderr = input, output, output
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		log.Fatal(err)
	}
	fmt.Println(command.Process.Pid)
	if err := command.Process.Release(); err != nil {
		log.Fatal(err)
	}
}
