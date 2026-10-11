# Contributing

This is a personal project and it does not take contributions. Pull requests are
switched off. Issues are open, but I'll probably ignore them.

If you fork it, the notes below explain how to build and test it.


## Build and test

Use the pinned `devenv` environment, so you do not install anything globally:

```sh
devenv tasks run dev:build
devenv tasks run dev:test
```

There is no CI. Before you deploy a change, run the system test:

```sh
scripts/dev/system-test
```

It builds the proxy from your working tree and runs the whole system in
isolation, against real OpenAI, with copies of the accounts you signed in to
your installed Codex Switch. It drives the Codex client inside the desktop app,
or `codex` on your `PATH` if you only have the CLI. If `pi` is on your `PATH`, it
drives Pi too. It takes about four minutes and about 30 model requests. It never
touches your installed proxy, menu app or Codex configuration. See
[docs/system-test.md](docs/system-test.md) for what it proves and where to look
when it fails.

`devenv tasks run dev:stage` builds a signed menu app under `dist/staged/`
without opening it. For a change to the menu, open the app and look at every
state you changed.

## Your own installation

Do not test against your installed proxy, menu app or account data. To update
them, use the steps in the README: `deploy/install.sh` hands the proxy over
without interrupting running agents.

To try a change to the install scripts, install a second copy beside your own:
set `HOME` to an empty directory, and set `CODEX_SWITCH_LAUNCHD_LABEL`,
`CODEX_SWITCH_LISTEN` and `CODEX_SWITCH_MENU_APP` to a label, a loopback port
and an app path of their own. Its menu app reads only that copy's proxy.
