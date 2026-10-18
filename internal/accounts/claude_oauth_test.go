package accounts

import (
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

func TestClaudeOAuthKeepsVerifierOutOfBrowserURL(t *testing.T) {
	proof, err := newClaudeOAuthProof()
	if err != nil {
		t.Fatal(err)
	}
	address := proof.authorizationURL("http://localhost:12345/callback")
	if strings.Contains(address, proof.verifier) {
		t.Fatal("browser authorization URL exposes PKCE verifier")
	}
	u, _ := url.Parse(address)
	q := u.Query()
	expected := sha256.Sum256([]byte(proof.verifier))
	if q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(expected[:]) || q.Get("code_challenge_method") != "S256" {
		t.Fatal("invalid PKCE challenge")
	}
	if !proof.validState(q.Get("state")) || proof.validState("") || proof.validState(proof.verifier) {
		t.Fatal("return state validation accepts an unrelated value")
	}
	next, _ := newClaudeOAuthProof()
	if proof.validState(next.state) {
		t.Fatal("authorization attempts share state")
	}
}
