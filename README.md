# Codex Switch

Codex Switch is a local macOS proxy that lets Codex use several ChatGPT
subscription accounts without manual account switching. It keeps each task on
one account and assigns new tasks to an eligible account.

Codex Switch preserves prompts, tool calls and request bodies. It supplies the
selected account's subscription credentials. When an account rejects a request
before starting work, the proxy lets Codex retry against another eligible account;
it never resubmits model work itself.

## Status

Work in progress. It relies on undocumented ChatGPT subscription endpoints,
which can change without notice.

## How routing works

- A new task uses the eligible account with the most remaining weekly capacity.
  Exact capacity ties use a stable task-ID-based ranking to spread new work across accounts.
- Later work for that task uses the same account.
- Each sub-agent has its own task ID and assignment.
- A model request without a task ID uses the account a new task would get and
  is never pinned. Metadata requests do not create assignments.
- After OpenAI confirms exhaustion, the next client request uses another
  eligible account. If none can serve the requested model, the request waits
  for capacity or client cancellation. The proxy does not substitute models.

## Build and test

Use the pinned devenv environment. These commands build and inspect repository
artifacts only. They do not launch, install or replace the running proxy or
menu app.

```sh
devenv tasks run dev:build
devenv tasks run dev:test
devenv tasks run dev:stage
devenv tasks run dev:inspect
```

`dev:stage` creates a signed app under `dist/staged/`. `dev:inspect` verifies the
bundle, menu executable and signature without opening the app.

## Deployment

The [system test](docs/system-test.md) exercises an update during work. A
private launchd job supervises a stable
executable path. The deployment script starts a detached bridge on the same
loopback port, confirms it accepts new connections, and lets admitted work
finish on the old process. It retires the bridge only after confirming the
supervised successor. Python 3 is included in the pinned development environment.

Build proxy artifacts with `devenv shell -- scripts/dev/build-proxy <output>`;
this is also the proxy build used by `dev:build` and the system test. It records source
revision and modified state for deployment checks.

Live proxy deployment, menu installation and main Codex configuration changes
require separate explicit approval. No isolated command adopts the installed
services or their writable data.

## Add accounts

Run `login` once for every account to add. The official Codex CLI performs each
interactive sign-in.

```sh
codex-switch login
codex-switch login
codex-switch accounts
```

Name an existing account only when it needs to sign in again:

```sh
codex-switch login account-2
```

Remove an account explicitly:

```sh
codex-switch remove account-2
```

Account data is stored under `~/Library/Application Support/codex-switch`.

## Configure Codex

Print the user-level setting for this installation:

```sh
codex-switch print-codex-config
```

Copy its single `openai_base_url` line into `~/.codex/config.toml`. The private
URL identifies this installation and prevents unrelated local applications
from sending requests through your subscription accounts.

Remove that line to use OpenAI's standard endpoint again. Codex must reload its
configuration before the change takes effect.

## Inspect current work

```sh
codex-switch
codex-switch status
```

Both commands read the snapshot the serving proxy composes once a minute. They
show each account's allowance, whether it can take new work and why not, and
recent tasks with the accounts that served them.

The serving proxy appends to `proxy.log` in its data directory; every process,
including both sides of a blue-green update, writes to the same file. Each line
has a timestamp and names the task, the client that sent it and the account
that served it. The log records when a task is assigned or moves to an account
and why, when work waits for an account, every request OpenAI rejects or fails
with OpenAI's reason, and when an account's availability changes. It does not
log routine successful requests, prompts or responses.

The model-request table counts attempts through this proxy from its displayed
start time. Each HTTP model POST or WebSocket model request counts once;
client retries count separately. Successful means the proxy observed a successful
terminal response. Failed means the provider explicitly rejected or failed the
request. Interrupted means the connection ended before a terminal response was
observed, including client cancellation. These are request outcomes, not task
outcomes. Warmups, connection setup, auxiliary calls and requests still waiting
for an account do not count. Compaction requests do count.

Counters are stored as daily account aggregates and survive normal proxy updates.
They begin when request observation is first enabled; earlier completed replies
are not used to invent earlier request or failure totals. The separate completed
replies and tokens table shows today's recorded work. An active request has no
outcome yet; a process crash or failed counter write does not manufacture one.

The menu app reads the same protobuf snapshot from the proxy's private URL.

## Local data

Codex Switch stores credentials, a private client capability, task IDs,
task-to-account assignments, daily request counts and completed-response token records. Current
allowance observations are held in memory. It does not store prompts, model
responses, reasoning text, authorisation headers or raw WebSocket messages.

## Licence

MIT. See `LICENSE`.

Codex Switch is an independent project and is not affiliated with OpenAI.
