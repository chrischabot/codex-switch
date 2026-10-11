# Security

Codex Switch has no releases. Security fixes go to the main branch.

## Report a vulnerability privately

Do not disclose a suspected vulnerability in a public issue or pull request.
Use **Report a vulnerability** in this repository's **Security** tab when that
control is available.

If private vulnerability reporting is not available, open a public issue asking
the maintainer for a private contact route. Include no vulnerability details in
that issue.

Describe the affected code, the impact and the shortest safe reproduction. Do
not include live credentials or private user data.

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
