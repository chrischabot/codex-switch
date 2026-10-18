package relay

import (
	"net/http"
	"strings"
	"testing"
)

func TestRejectionDiagnosticsNeverRetainProviderText(t *testing.T) {
	secret := "private-token-and-prompt"
	for _, body := range []string{
		`{"error":{"type":"server_error","message":"` + secret + `"}}`,
		`{"error":{"type":"` + secret + `","message":"irrelevant"}}`,
		`{"detail":"` + secret + `"}`,
		`<title>` + secret + `</title>`, secret,
	} {
		for _, contentType := range []string{"text/html", "application/json"} {
			headers := http.Header{"Content-Type": {contentType}}
			reason := describeRejection(headers, []byte(body))
			if strings.Contains(reason, secret) {
				t.Fatalf("provider text leaked: %s", reason)
			}
		}
	}
	headers := http.Header{"Cf-Mitigated": {secret}, "Cf-Ray": {secret}}
	if reason := describeRejection(headers, nil); strings.Contains(reason, secret) {
		t.Fatalf("untrusted header leaked: %s", reason)
	}
	reason := describeRejection(http.Header{}, []byte(`{"error":{"type":"server_error","message":"`+secret+`"}}`))
	if !strings.Contains(reason, "server_error") {
		t.Fatal("lost safe error classification")
	}
}
