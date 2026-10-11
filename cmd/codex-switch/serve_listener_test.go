package main

import "testing"

func TestServeListenerAcceptsOnlyNumericLoopbackTCP(t *testing.T) {
	for _, address := range []string{"127.0.0.1:2455", "127.0.0.1:0", "[::1]:2455"} {
		if err := validateLocalListenAddress(address); err != nil {
			t.Fatalf("loopback %q rejected: %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:2455", "[::]:2455", "192.168.1.10:2455", "localhost:2455", "2455"} {
		if err := validateLocalListenAddress(address); err == nil {
			t.Fatalf("non-numeric or non-loopback address %q accepted", address)
		}
	}
}
