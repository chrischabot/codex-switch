<!-- Written by AI on 2026.10.11 by model claude-opus-5.5; updated on 2026.10.12 by model GPT-6.1 Sol. -->

# System test

Run this before a live deploy:

```sh
scripts/dev/system-test
```

It takes about four minutes. It builds the proxy from this working tree, runs
the ChatGPT pool in isolation, and prints what the system did for each outcome:

1. New Codex tasks and Pi sessions go to the account with the most weekly
   allowance left, and stay there when another account takes the lead.
2. A request without a task ID is served.
3. When a task's account runs out, its next turn moves to another account. No
   model work is replayed, no tool effect repeats, and Codex shows no error.
4. With every account out of allowance, a running goal continues by itself
   when one account comes back.
5. A blue-green update during work completes by itself. Responses in flight
   over WebSocket and HTTP finish on the old process, while new work starts
   on the new one.
6. Each completed response is recorded once, with its account, task and
   tokens.

The command exits 0 only if every outcome held and nothing is left running.

## What is real

- The proxy binary built from this tree, under its own launchd job, on a spare
  loopback port, with its own data directory.
- The official Codex client: the one inside the desktop app
  (`/Applications/ChatGPT.app/Contents/Resources/codex-cli/bin/codex`), or
  `codex` on `PATH` without the app. It runs `codex exec` tasks and `/goal`
  through `codex app-server`.
- Pi in sessions, if `pi` is on `PATH`. Without it the test says so at the
  start and runs everything else.
- OpenAI, with your signed-in accounts, on `gpt-6-luna` at low effort. One run
  uses about 30 model requests and 0.5 million input tokens, mostly cached.
- The update path: `deploy/deploy.sh new` against the private launchd job.

## What is simulated

`scripts/dev/system-test-gateway` stands between the proxy and OpenAI. It
forwards real traffic and logs a fingerprint of every model request it sends,
so the test can see that none was sent twice. Its control file simulates what a
test cannot wait for: a weekly allowance level, an account that runs out after
a given number of requests, refused WebSockets, and responses held mid-stream
for one client. The update installs a copy of the same build, so the old and
new processes differ by process, not by source.

## Safety

The test reads the account logins from the live data directory and copies only
the access tokens. Refresh tokens stay behind, so the test cannot rotate a live
login. Each account needs an access token valid for at least another hour.

It never touches the live proxy, its launchd job or data directory,
`~/.codex/config.toml`, Pi's configuration or the menu app. Codex and Pi run
with their own homes inside the run directory. When the run ends, or fails, the
test removes its launchd job (`dev.codex-switch.system-test`) and stops every
process it started, then checks that none remain.

## When an outcome fails

The failure line says what happened instead. The evidence of the last run is
in `.project-private/system-test/`:

| File | Contains |
| --- | --- |
| `data/proxy.log` | Which account each task got and why, moves, waits and drains |
| `data/codex-switch.db` | Task pins and recorded responses |
| `logs/gateway.log` | Every model request sent to OpenAI, refusal and held response |
| `logs/<task>.out`, `logs/<goal>.jsonl` | What Codex, Pi and the update command printed |
| `home/sessions/` | Codex's own record of each response it received |
| `control` | The simulation as it ended |

The next run moves this directory to the Trash.

## Claude accounts

[Claude setup](../README.md#claude-subscriptions) uses the same proxy, data
folder and account commands. Sign in with `login --claude`; print Claude Code's
persistent settings with `print-claude-config`, or Pi's provider entry with
`print-claude-config --pi`.

The system-test command above exercises ChatGPT accounts. Test Claude Code and
Pi against a separate proxy and data folder, using `--listen` on the printed
settings to match that proxy's port. Use new Claude sign-ins in that folder.
Keep existing client settings and live account tokens in place. Check session
placement, continuation, forks and cache reads, model-specific limits, usage
and a blue-green update with the real clients.
