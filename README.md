# Codex Switch

Use several ChatGPT accounts in Codex as one pool, without ever logging out.

<img src="docs/images/menu-demo.png" alt="The Codex Switch menu cycling through Today, 7 Days and 30 Days, in API-equivalent cost and then in tokens, with account emails hidden" width="344">

## Why

When GPT-5.6 Sol came out, one or two Codex threads could use up a five-hour
limit in one to two hours. I had two accounts and balanced work between them by
hand. It wasn't enough.

The ChatGPT app is built for one account at a time. To use a second account you
log out and log in again, and that breaks things: Codex Remote stops working,
memory stops working, and running tasks lose their place. There is no way to
pool usage across accounts.

Codex Switch is a transparent proxy. You stay logged in to your main ChatGPT
account everywhere, and Codex Switch spreads the work across all your accounts
behind the scenes.

Ironically, Claude Opus 5.5 is what finally let me ship it.

## How it works

Codex lets you set a different API address. Companies use the same setting to
put their own gateway in front of OpenAI. Codex Switch sits at that address as a
drop-in, so the ChatGPT app and the Codex CLI just work. Nothing is patched.

- It all runs locally on your Mac. Need it on two machines? Install it on both.
- You sign each account in once, with Codex's normal ChatGPT login. Codex Switch
  keeps every account's tokens refreshed after that.
- It does not use OpenAI's "Sign in with ChatGPT" for apps yet. That may come
  later.

## Is this allowed?

Use it at your own risk. You're probably not supposed to do this: it relies on
ChatGPT endpoints that OpenAI doesn't document, and pooling accounts may go
against their terms. Until OpenAI supports several accounts in the app natively,
it's the only way I've found to make this work. Tibo, if you're reading this:
please add it, fix the limits, and ship a model better than Opus 5.5. lol

It is macOS only, you build it from source, and so far it has one user: me.

## What it does

- Codex (the app, the CLI and sub-agents) and [Pi](https://github.com/badlogic/pi-mono)
  all point at one local URL.
- A new task goes to whichever account has the most weekly allowance left.
- A task stays on its account, so its prompt cache stays warm.
- When an account runs out, the task moves to another account on its next turn.
  Nothing is replayed and the agent just carries on.
- Updating the proxy never drops work in progress.
- The menu bar shows what's left on each account, what each account and task
  used, and that the pool is working.

## The menu

The menu bar shows the weekly allowance left on each account.

<img src="docs/images/menu-bar-item.png" alt="Menu bar item: account 1 at 96% and account 2 at 96%" width="140">

Click it for:

- whether the pool is working, and how many accounts are in it
- cost at API prices, or tokens, for Today, 7 Days or 30 Days, split by account
- each account's allowance, when it resets, and its usage over the last 30 days
- when an account will run out, if it's going to run out before it resets
- the tasks that used the most, and which accounts served them

The eye button hides emails so you can post screenshots. Account 3 below is a
free account that can't run the model I use, so the work goes to the other two.

<img src="docs/images/menu-panel.png" alt="The full menu for Today in API-equivalent cost, with emails hidden and task names blurred" width="344">

Account totals come from OpenAI's own daily usage report for each account, so
they include work done on other machines. Task totals come from the Codex and Pi
session logs on your Mac.

## Install

The easy way: paste this into Codex or Claude Code and let it do the work.

```text
Install Codex Switch from https://github.com/joshp123/codex-switch on this Mac.
Read its README first. Clone it, run deploy/install.sh, then help me sign in
each of my ChatGPT accounts with `codex-switch login` (I'll do the browser
part). Add the line from `codex-switch print-codex-config` to the top of
~/.codex/config.toml, and to Pi's models.json if I use Pi. Finish by running
`codex-switch status` and show me that every account is available.
```

The only thing you do yourself is sign in to each account in the browser.

### Doing it by hand

You need:

- macOS 14 or later
- Go 1.24 or later, for the proxy
- Xcode 16 or later, for the menu bar app
- Codex, either the desktop app or the CLI
- two or more ChatGPT subscriptions

```sh
git clone https://github.com/joshp123/codex-switch.git
cd codex-switch
deploy/install.sh
```

This builds the proxy and runs it under launchd on `127.0.0.1:2455`, so it
starts at login and restarts if it stops. It builds the menu bar app, signs it
for this Mac only, copies it to Applications and opens it. You do not need an
Apple developer account. From then on the app opens at login.

It also puts the `codex-switch` command in `~/.local/bin`. Make sure that is on
your `PATH`.

### 1. Sign in to each account

Run `login` once for each account. Each run opens the official Codex sign-in in
your browser. Sign in with a different account each time.

```sh
codex-switch login
codex-switch login
codex-switch accounts
```

`accounts` lists what you added:

```text
account-1	you@example.com
account-2	you+work@example.com
```

### 2. Point Codex at it

`deploy/install.sh` ends by printing one line:

```toml
openai_base_url = "http://127.0.0.1:2455/client/<private key>/backend-api/codex"
```

Add it to the top of `~/.codex/config.toml`, above any `[section]`, then
restart Codex. `codex-switch print-codex-config` prints it again. The URL
contains a private key for your installation, so web pages and other apps that
do not know it cannot use your accounts through the proxy.

To go back to OpenAI directly, delete that line and restart Codex.

For Pi, add the same URL to `~/.pi/agent/models.json`:

```json
{
  "providers": {
    "openai-codex": {
      "baseUrl": "<the URL from print-codex-config>",
      "apiKey": "eyJhbGciOiJub25lIn0.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL2F1dGgiOnsiY2hhdGdwdF9hY2NvdW50X2lkIjoiY29kZXgtc3dpdGNoIn19.codex-switch"
    }
  }
}
```

The `apiKey` is a placeholder. Pi needs one to start, and the proxy replaces it
with the real account's sign-in.

### Check it works

```sh
codex-switch status
```

This shows each account's allowance, whether it can take new work and why not,
and recent tasks with the accounts that served them. The proxy's log is
`~/Library/Application Support/codex-switch/proxy.log`. It records which
account each task got and why, every move, every wait and every request OpenAI
rejected. It never records prompts or responses.

## How routing works

- New tasks go to the account with the most weekly allowance left. Every
  sub-agent counts as its own task.
- A task stays on its account until OpenAI says that account has run out. Its
  next request then goes to another account.
- If OpenAI rejects a request before any work starts, Codex retries it like it
  normally would, and the retry lands on another account. The proxy never
  resends anything itself.
- If no account can run the model you asked for, the request waits until one
  can. The proxy never swaps your model for a different one.

Add an account any time with `codex-switch login`, and remove one with
`codex-switch remove account-2`. No restart needed.

## How updates work

Run the install script again:

```sh
git pull
deploy/install.sh
```

A new proxy process starts on the same port and takes all new requests. The old
process stops accepting work, finishes every request it already has, including
streams and WebSockets, then exits, and launchd runs the new build from then
on. Running agents carry on. The script then quits, rebuilds and reopens the
menu app.

To go back to the previous proxy:

```sh
deploy/deploy.sh back \
  --stable ~/.local/lib/codex-switch/codex-switch \
  --data ~/Library/Application\ Support/codex-switch \
  --listen 127.0.0.1:2455
```

## Uninstall

First point Codex back at OpenAI: delete the `openai_base_url` line from
`~/.codex/config.toml`, and the `openai-codex` provider from Pi's
`models.json`, then restart them. Then:

```sh
deploy/uninstall.sh
```

The proxy finishes the requests it has already accepted and stops. The script
removes its launchd job, the `codex-switch` command and the menu app. Your
sign-ins and usage history stay in `~/Library/Application Support/codex-switch`;
move that folder to the Trash to remove them too.

## Compared with the alternatives

| | What it is | Lines of code |
| --- | --- | ---: |
| **Codex Switch** | Account pool, menu bar app, per-task usage, updates that don't drop work | **about 8,500** (Go and Swift) |
| [codex-lb](https://github.com/Soju06/codex-lb) | Account pool with a web dashboard; retries and replays requests inside the proxy | about 226,000 (Python and TypeScript) |
| [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) | Gateway for lots of AI providers, translating between their APIs | about 232,000 (Go) |
| [opencodex](https://github.com/lidge-jun/opencodex) | Proxy from Codex and Claude Code to any model provider | about 504,000 (TypeScript) |

The small `auth.json` switchers are simpler, but they switch accounts by
restarting Codex, which kills every running agent.

Codex Switch is small because it does one thing: pool ChatGPT accounts for one
person on one Mac. Everything else is left to Codex and Pi. It has six direct
dependencies.

Line counts exclude tests, generated and vendored code, comments and blank
lines, measured with [scc](https://github.com/boyter/scc) on 11 October 2026.

## Local data

Everything lives in `~/Library/Application Support/codex-switch`: your account
sign-ins, the private URL key, which task is on which account, and token counts.
No prompts, responses or raw traffic are stored. See [SECURITY.md](SECURITY.md).

## Contributing

This is a personal project. Pull requests are switched off. Issues are open, but
I'll probably ignore them.

## Credits

The OpenAI mark in the menu bar and much of the menu's design come from
[OpenUsage](https://github.com/robinebers/openusage) by Robin Ebers, used under
the MIT licence (copyright 2026 Robin Ebers).

Codex Switch is not affiliated with OpenAI. OpenAI, ChatGPT and Codex are
trademarks of OpenAI.

## Licence

MIT. See [LICENSE](LICENSE).
