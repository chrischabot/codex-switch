# Contributing

Codex Switch does one thing: it lets Codex use several ChatGPT accounts as one
pool, shows exactly what each account and task used, and updates without
interrupting running agents. Keep changes to that.

## Build and test

Use the pinned `devenv` environment, so you do not install anything globally:

```sh
devenv tasks run dev:build
devenv tasks run dev:test
```

There is no CI. Before you open a pull request, run the system test:

```sh
scripts/dev/system-test
```

It builds the proxy from your working tree and runs the whole system in
isolation, with the real Codex and Pi clients and your own signed-in accounts.
It takes about four minutes and about 30 model requests. It never touches your
installed proxy, menu app or Codex configuration. See
[docs/system-test.md](docs/system-test.md) for what it proves and where to look
when it fails.

`devenv tasks run dev:stage` builds a signed menu app under `dist/staged/`
without opening it. For a change to the menu, open the app and look at every
state you changed.

## Your own installation

Do not test against your installed proxy, menu app or account data. Do not
restart or replace them while agents are running. To update them, use the steps
in the README.

## Changes

- Keep one obvious way to do each thing, with clear, searchable names.
- Do not add compatibility code for designs that never shipped.
- Do not show uncertainty as warnings, caveats or ranges. Find the source of
  truth, or remove the feature.
- Preserve request bodies, responses and streaming exactly when changing the
  proxy.
- Add a test only when it protects a real user path, a hard external contract
  or a small critical rule.

A pull request should say what changes for the user, how you proved it and the
commands that passed. For menu changes, include before and after screenshots
taken in privacy mode.

Keep credentials, the private URL key, account emails and IDs, task names and
anything else from your own Codex use out of commits, logs and screenshots.
