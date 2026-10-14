<!-- Written by AI on 2026.10.14 by model GPT-6.1 Sol. -->

# Updating through a network fault

A candidate performs a fresh authenticated allowance read for every signed-in
account from its own process before the incumbent stops accepting work. Cached
allowances do not satisfy this check. A failed read exits the candidate and
leaves the incumbent and stable binary in place. A successful check permits the
existing handover: the incumbent finishes admitted work while the candidate
serves new requests.

The command identifies the failing provider and account, reports the connection
error, and says `old generation keeps serving`. The proxy log records
`provider check failed`. The incumbent has no corresponding stopping log.

## Connection and resend policy

Upstream HTTP uses HTTP/1.1 with keep-alive: one active request occupies each
connection. Concurrent requests use separate sockets. WebSocket model traffic
already has a separate upstream connection for each client connection. This
removes the shared HTTP/2 socket that coupled unrelated sessions. It also
removes Go's HTTP/2 automatic `PROTOCOL_ERROR` retry, which does not prove that
the provider refused model work.

| Fault | Proxy action |
| --- | --- |
| Dial or TLS I/O fails before a connection is acquired | Send again once; no HTTP request was written. |
| A reused connection fails before writing any request bytes | Let Go's transport rewind the buffered model body and send again, unless the request has an idempotency header. |
| The provider could have received request bytes | Do not resend model work. Return `502 upstream service unavailable` before response headers; close an interrupted stream after headers. |
| HTTP/2 refusal, excluded stream in GOAWAY, or protocol error | Absent from the proxy's HTTP/1.1 upstream transport. |

Model requests do not grant Go permission to replay through idempotency
headers. Logs name the task and account and say whether the proxy resent the
request. Client-owned recovery remains the client's decision, as on a direct
connection.

## Client evidence

These rules were checked in the installed clients and their versioned source.
They establish recovery policy and connection ownership. Direct provider ALPN
negotiation was not measured.

| Client | Direct behavior |
| --- | --- |
| Codex 0.162.1 | HTTP retries all 5xx and timeout, connection and network errors: four retries by default, with a separate five-retry stream recovery budget. HTTP retries honor `RetryAfter`; ordinary HTTP 429 is excluded from that layer. OpenAI uses session-owned WebSockets and session-owned HTTP fallback. |
| Claude Code 2.1.296 | Main model requests set SDK retries to zero and use Claude Code's outer recovery loop. Default: ten retries, capped at fifteen. The watchdog raises transient-fault retries to 300 and waits through sustained 429/529 capacity faults without an attempt cap. The loop retries connection failures, 408, 409, 429 and 5xx, including 502. Recovery after streamed output can retain partial output. Each CLI process owns its fetch connections. |
| Pi 0.79.8 | Defaults to session-owned WebSockets, falling back to SSE before the message stream starts. Provider HTTP retries default to zero. Agent recovery defaults to three retries after 2, 4 and 8 seconds; its error matching includes 502, service unavailable, connection loss and premature stream endings. |

The existing 502 status and text are retryable for all three clients. Provider
response headers and bodies continue to pass through, including provider retry
instructions.

Source references:

- Codex: [HTTP retry rules and delay](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/codex-client/src/retry.rs#L20),
  [provider defaults and policy](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/model-provider-info/src/lib.rs#L59),
  [session connection ownership](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/core/src/client.rs#L242).
- Claude Code: [retry limits and watchdog](https://code.claude.com/docs/en/env-vars).
  The installed 2.1.296 bundle also contains the main request's `maxRetries:0`,
  outer status rules and partial-stream recovery decisions.
- Pi: [transport and provider retry rules](https://github.com/earendil-works/pi/blob/v0.79.8/packages/ai/src/providers/openai-codex-responses.ts#L230),
  [agent retry defaults](https://github.com/earendil-works/pi/blob/v0.79.8/packages/coding-agent/src/core/settings-manager.ts#L752),
  [agent retry decisions](https://github.com/earendil-works/pi/blob/v0.79.8/packages/coding-agent/src/core/agent-session.ts#L2315).
  The installed packages are `@earendil-works/pi-coding-agent` and
  `@earendil-works/pi-ai` 0.79.8.

## Repeatable proof

Build and run the repository checks:

```sh
devenv tasks run dev:build
devenv tasks run dev:test
devenv shell go test -race ./cmd/codex-switch -run 'Test(DeployProcessChecksProvidersBeforeTakingTraffic|ProxyProcessNetworkFaults)' -v
```

The process tests build the proxy and use isolated data, synthetic logins,
loopback providers and a TCP relay. They run the shipping `deploy.sh new`,
reject a candidate whose Claude provider is unreachable, verify the incumbent
still serves, then complete a successful update while admitted model work is
held open. Network tests reset a real socket after a provider receives the model
body, check the resend decision, and check that another concurrent request
completes without a duplicate provider admission.

The TLS transport test uses explicit fixture roots and a server offering
HTTP/2 to check successful recovery before TLS and HTTP/1.1 negotiation. A
second transport test injects a zero-byte socket write failure on a real reused
TCP connection. It proves native Go recovery and exactly one provider admission
per request; that injection is not a remote TCP reset.

```sh
devenv shell go test -race ./internal/relay -run 'TestUpstreamTransportResends(BeforeTLSHandshakeWithoutHTTP2|AfterZeroByteWriteOnReusedSocket)' -v
```

The installed client checks are opt-in. Set `CODEX_SWITCH_TEST_CODEX_EXECUTABLE`,
`CODEX_SWITCH_TEST_PI_EXECUTABLE` and `CODEX_SWITCH_TEST_CLAUDE_EXECUTABLE` to the
direct packaged executables, then run:

```sh
devenv shell go test -race ./cmd/codex-switch -run 'TestReal(Codex|Pi|Claude)ClientRecoversAfterProxyConnectionReset' -v
```

These checks passed with all three installed clients. Each uses isolated client
directories, synthetic credentials and an SSE fixture. Codex and Pi use HTTP
for this check; Claude reaches the proxy through HTTPS CONNECT with a temporary
CA. The tests reset the upstream socket after the full client POST arrives,
verify the proxy returns 502 without resending, and check that the client's own
retry receives the fixture's completed answer. Pi records successful automatic
recovery; Claude records a 502 retry followed by success; Codex completes its
turn.

Supply Pi's packaged executable rather than a user launcher that can load
credential helpers. The tests sanitize the environment and use file credentials
for Codex and Pi, and Claude's bare startup with an explicit synthetic API key.
They do not change client configuration or read keychain secrets.

The sandbox proof uses local providers, not authenticated public provider
requests. The lead must repeat provider reads, client recovery and blue-green
handover with signed-in accounts in an isolated installation. Direct provider
ALPN and default WebSocket fault recovery are outside the measured path. These
tests do not change or deploy the live installation.
