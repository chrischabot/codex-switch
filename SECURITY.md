# Security

This is a personal project with no security support. Use it at your own risk.

## Sensitive local data

Treat these as credentials:

- ChatGPT access, refresh and identity tokens
- the private URL key (`client-capability`) and any URL containing it
- account IDs and email addresses
- files under `~/Library/Application Support/codex-switch`
- private Codex configuration, tasks, prompts and responses

Use isolated accounts, data directories and ports for reproduction. Redact
sensitive values from logs, screenshots and test fixtures. A loopback listener
is not permission to expose subscription credentials to other local software.
