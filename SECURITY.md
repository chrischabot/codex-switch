# Security

## Sensitive local data

Treat these as credentials:

- ChatGPT and Claude access, refresh and identity tokens
- the private URL key (`client-capability`) and any URL containing it
- the Claude proxy authority private key (`claude-proxy-authority.pem`)
- account IDs and email addresses
- files under `~/Library/Application Support/codex-switch`
- private client configuration, tasks, prompts and responses

Use isolated accounts, data directories and ports for reproduction. Redact
sensitive values from logs, screenshots and test fixtures. A loopback listener
is not permission to expose subscription credentials to other local software.
