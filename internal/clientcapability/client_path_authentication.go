package clientcapability

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

const clientPathPrefix = "/client/"

// PathPrefix is the private URL prefix a local client must put in front of
// every Codex Switch request.
func PathPrefix(capability string) string {
	return clientPathPrefix + capability
}

// RequirePathPrefix requires /client/{capability} at the start of every
// request path and removes those two path segments before passing the request
// to next. Any other path is answered with 404 without reaching next.
func RequirePathPrefix(capability string, next http.Handler) http.Handler {
	if len(capability) != encodedCapabilityLength {
		panic("client capability must be 43 characters")
	}
	if next == nil {
		panic("client authentication requires a next handler")
	}

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		remainder, authenticated := authenticatedRemainder(request.URL.Path, capability)
		if !authenticated || !escapedPrefixIsExact(request.URL.EscapedPath(), capability) {
			http.NotFound(response, request)
			return
		}

		forwarded := request.Clone(request.Context())
		forwarded.URL.Path = remainder
		forwarded.URL.RawPath = ""
		forwarded.RequestURI = forwarded.URL.EscapedPath()
		if forwarded.URL.RawQuery != "" {
			forwarded.RequestURI += "?" + forwarded.URL.RawQuery
		}
		next.ServeHTTP(response, forwarded)
	})
}

func authenticatedRemainder(requestPath, capability string) (string, bool) {
	if !strings.HasPrefix(requestPath, clientPathPrefix) {
		return "", false
	}
	afterPrefix := requestPath[len(clientPathPrefix):]
	segmentEnd := strings.IndexByte(afterPrefix, '/')
	if segmentEnd == -1 {
		segmentEnd = len(afterPrefix)
	}
	presentedCapability := afterPrefix[:segmentEnd]
	if len(presentedCapability) != encodedCapabilityLength ||
		subtle.ConstantTimeCompare([]byte(presentedCapability), []byte(capability)) != 1 {
		return "", false
	}
	if segmentEnd == len(afterPrefix) {
		return "/", true
	}
	return afterPrefix[segmentEnd:], true
}

func escapedPrefixIsExact(escapedPath, capability string) bool {
	_, authenticated := authenticatedRemainder(escapedPath, capability)
	return authenticated
}
