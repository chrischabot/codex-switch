# Contributing

Codex Switch is an unreleased macOS project. Keep changes focused on its one
purpose: route Codex work across a user's ChatGPT accounts, show exact usage,
and update without interrupting running agents.

## Development

Use the pinned `devenv` environment. Do not install dependencies globally.

```sh
devenv tasks run dev:build
devenv tasks run dev:test
```

`dev:test` is the canonical Go and Swift test path. Add a test only when it
protects a real user path, a hard external contract or a small critical rule.

Run the [system test](docs/system-test.md), `scripts/dev/system-test`, to
prove the whole system with the real clients before a live deploy.
Build/signature checks are available through `dev:stage` and `dev:inspect`;
they do not prove product behavior.

Never use the installed proxy, menu, account data or port 2455 as a development
test target. Do not install, restart or replace live components while agents
are running.

## Changes

- Use explicit, searchable names and keep one obvious implementation path.
- Do not add compatibility code for unreleased designs.
- Do not expose uncertainty as warnings, caveats, ranges or explanatory UI.
  Establish the source of truth or remove the unsupported feature.
- Preserve request bodies, responses and streaming behaviour when changing the
  proxy.
- Inspect the rendered app for UI changes. Include before-and-after screenshots
  with the pull request.

A pull request should explain the user outcome, the human path used to prove it
and the exact commands that passed. Keep credentials, client capabilities,
account identifiers and private Codex data out of commits, logs and screenshots.
