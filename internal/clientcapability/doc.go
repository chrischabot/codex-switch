// Package clientcapability owns the private /client/<capability> URL prefix
// that proves a request comes from a local client of this user. It creates and
// reads the secret in the data directory and rejects requests without it.
package clientcapability
