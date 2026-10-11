package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestCommandSelectionMakesStatusDefaultAndServeExplicit(t *testing.T) {
	command, args := selectCommand(nil)
	if command != "status" || len(args) != 0 {
		t.Fatalf("default = %q %#v", command, args)
	}
	command, args = selectCommand([]string{"serve", "--listen", "127.0.0.1:9999"})
	if command != "serve" || strings.Join(args, " ") != "--listen 127.0.0.1:9999" {
		t.Fatalf("serve = %q %#v", command, args)
	}
	command, args = selectCommand([]string{"status", "--address", "127.0.0.1:9999"})
	if command != "status" || !reflect.DeepEqual(args, []string{"--address", "127.0.0.1:9999"}) {
		t.Fatalf("explicit status command = %q %#v", command, args)
	}
	command, args = selectCommand([]string{"print-codex-config"})
	if command != "print-codex-config" || len(args) != 0 {
		t.Fatalf("Codex config command = %q %#v", command, args)
	}
}
