// Package relay forwards Codex HTTP, SSE, and WebSocket traffic without replaying
// model work or retaining request and response content. It changes only the
// Authorization and ChatGPT account request headers.
package relay
