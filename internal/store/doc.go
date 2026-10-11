// Package store owns Codex Switch's shared domain types and one SQLite database.
//
// It persists routing facts and the responses the relay completed: no credentials,
// allowance snapshots, rates, provider abstraction, migrations, retries, or response
// content. It imports no other Codex Switch package; the usage package keeps its own
// tables in the same database. SQLite is provided by modernc.org/sqlite so the proxy
// stays a single CGO-free binary.
package store
