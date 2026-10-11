// Package store owns Codex Switch's shared domain types and one SQLite database.
//
// It persists routing and accounting facts only: no credentials, allowance snapshots,
// rates, provider abstraction, migrations, retries, or response content. SQLite is
// provided by modernc.org/sqlite so the proxy stays a single CGO-free binary.
package store
