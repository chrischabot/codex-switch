# Isolated account continuity rig

Written by AI on 2026.09.05 by model GPT-6 Astra.

The rig runs the bundled official Codex client, this checkout's proxy and a local upstream gateway. It never starts or changes the installed proxy, menu or shared Codex daemon. Every listener uses an allocated loopback port.

```sh
export CODEX_SWITCH_RIG_ROOT="$PWD/.project-private/my-continuity-run"
scripts/dev/continuity-rig prepare
scripts/dev/continuity-rig build
scripts/dev/continuity-rig start
scripts/dev/continuity-rig exec
scripts/dev/continuity-rig resume 'Read input.txt and result.txt with a tool and verify the sum.'
scripts/dev/continuity-goal.py --model gpt-6-astra
scripts/dev/continuity-rig stop
```

Run from this checkout. `prepare` creates separate `home`, `work`, `data` and `logs` directories. It reads the three installed account auth files by value, replaces refresh tokens with an unusable value, and checks that access tokens have at least an hour remaining. It copies no history, plugins or configuration. These files are private and must not be committed. Use a fresh directory for an independent exercise.

The client is the one bundled with the desktop app, `/Applications/ChatGPT.app/Contents/Resources/codex-cli/bin/codex`. `scripts/dev/continuity-rig pi 'prompt'` runs Pi through the same isolated proxy. The Nix CLI examined during recovery lacked its companion code-mode host and could not execute tools. The bundled client includes that host. The app-server goal driver uses its own stdio process, never the shared daemon.

`stop` signals only processes whose command matches this rig's exact binary path. It lets the proxy drain before stopping the upstream gateway. `prepare`, `build` and `start` refuse while those processes run.

## Exhaustion and restoration

Empty `control` means real upstream behavior. Account controls are one per line:

- `limited account-1`: simulated exhausted allowance and rejected model request.
- `reject account-1`: real allowance but a simulated rejection at the next HTTP request or WebSocket handshake.
- `reject-after account-1 1`: allow one real generating WebSocket message, finish its response, then reject the next message before forwarding it upstream. The gateway changes that account's line to `limited` when rejection happens.

Add a standalone `http-only` line to return HTTP 426 for WebSocket upgrades. The official client then uses HTTP Responses requests, with the same account controls.

A `weekly-used account-1 60` line makes that account's real allowance read report 60% of the weekly window used, so a run can choose which account has the most allowance left. The proxy rereads allowance once a minute.

Only `reject-after` terminates WebSocket framing in the gateway and disables compression. Accepted message payloads are unchanged. Other traffic uses a byte-preserving reverse proxy. Logs distinguish real upstream admissions from simulated rejections and use account aliases rather than credentials.

To exercise all three accounts and restore one during the same goal:

```sh
cat > "$CODEX_SWITCH_RIG_ROOT/control" <<'CONTROL'
reject-after account-1 1
reject-after account-2 1
reject-after account-3 1
CONTROL
scripts/dev/continuity-goal.py --restore-delay 30
```

The driver creates one official goal and sends its initial `turn/start` once. After all accounts become simulated-limited it waits the requested seconds and restores account-1 by changing only the simulation file. It never restarts or resumes the goal. It observes the goal for up to three minutes (or the requested restoration delay plus two minutes) and returns failure unless the goal completes, the sum is correct and the tool-effect file contains exactly one `START` line. Use `--restore-after-limit` to demonstrate that a goal already stopped as `usageLimited` does not recover merely because allowance returns.

To repeat automatic compaction, start a fresh rig, prepend the client setting, then start the goal:

```sh
export CODEX_SWITCH_RIG_ROOT="$PWD/.project-private/automatic-compaction"
scripts/dev/continuity-rig prepare
scripts/dev/continuity-rig build
scripts/dev/continuity-rig start
python3 - <<'PYTHON'
import os
from pathlib import Path
root = Path(os.environ['CODEX_SWITCH_RIG_ROOT'])
config = root / 'home/config.toml'
config.write_text('model_auto_compact_token_limit = 6000\n' + config.read_text())
(root / 'control').write_text('reject-after account-1 1\nreject-after account-2 1\n')
PYTHON
scripts/dev/continuity-goal.py
scripts/dev/continuity-rig stop
```

For the repeated HTTP rejection case, use a fresh rig with `http-only`, `reject account-1` and `reject account-2` as its three control lines, then run the same goal driver.

Raw app-server events are in `logs/goal-events.jsonl`; prior event logs are retained with timestamp prefixes. `logs/upstream.log` records admissions and rejections. `data/proxy.log` records which account each task was assigned to and why, waits, rejections and failures with OpenAI's reason, and account availability changes. `data/codex-switch.db` records task pins, exhaustion observations and completed responses. The driver prints the goal identity so those records can be joined. The private work directory contains the actual tool outputs.

## Candidate behavior

The existing account reader, durable pins, completion ledger and relay remain the operating path. When no account is available, model requests wait before upstream admission and recheck allowance every five seconds until capacity returns or the client disconnects. Routing now checks pinned-account availability for new requests, excludes known exhausted accounts from fallback and lets a newer successful allowance read clear older exhaustion evidence.

An explicit HTTP 429 `usage_limit_reached` rejection records exhaustion and returns the existing retryable 502 response. This covers both WebSocket handshakes and HTTP model requests. Other 429 responses retain their status and body.

For a WebSocket usage-limit rejection before `response.created`, the proxy records exhaustion and closes the connection without delivering the terminal limit. Codex reconnects and supplies continuation itself. The proxy never resends a request. Completed messages keep their original bytes. Limits after `response.created`, including `response.failed`, remain visible to the client because work may already have been admitted.

The exhaustion table now includes the observation time. This isolated candidate uses a fresh database; no migration or live deployment is included. Menu and accounting integration remain outside this phase.

## Observed results

On 5 September 2026, the bundled official client reported version 0.153.1.

- Real Luna work read the input, wrote `16`, and resumed the same task to verify it.
- A real active goal crossed all three accounts after two simulated established-WebSocket rejections and completed. Its append effect occurred once.
- With all three accounts simulated exhausted, restoration after 30 seconds let the same goal finish. The refreshed account's stored exclusion cleared.
- With a longer outage, the client exhausted five WebSocket and five HTTP retries, then marked the goal `blocked` with `willRetry: false`. Restoration alone did not start another turn. This was the baseline before adding the capacity wait.
- After adding a cancellable wait before upstream admission, a 90-second outage passed: WebSocket attempts timed out, HTTP fallback waited, and the same goal completed when account-1 returned. A separate official CLI interruption canceled its capacity wait. The manager independently repeated the outage for seven minutes: the same goal completed at 451 seconds with result `57` and one append effect.
- Two consecutive rejected WebSocket handshakes reached the third account and the goal completed. A separate run forced HTTP fallback with 426, rejected two POST requests with 429, and completed on account-3 in 25 seconds with one append effect and the correct sum.
- Automatic compaction was triggered with `model_auto_compact_token_limit = 6000`. The first compaction request was rejected before admission on account-1, completed on account-2, and subsequent work moved to account-3. Three compactions completed before the goal completed in 88 seconds, with result `16` and one append effect. No manual compaction request was sent.
- A real Astra goal in a second independent rig directory completed, with the expected sum and exactly one append effect.

Exhaustion in these runs was simulated, not an actual provider account limit. Normal model responses and tools were real. Recovery covers explicit pre-admission HTTP and WebSocket usage-limit rejections. It does not replay admitted failures or change limits delivered in an admitted SSE stream. Protocol tests protect fragmentation, interleaved ping, compression history and the boundary after `response.created`. The manager must independently exercise this candidate before accepting the phase.

## Client timeout boundary

The official `rust-v0.153.1` source sets ordinary provider requests to [`timeout: None`](https://github.com/openai/codex/blob/rust-v0.153.1/codex-rs/codex-api/src/provider.rs#L77). The [Responses streaming path](https://github.com/openai/codex/blob/rust-v0.153.1/codex-rs/codex-api/src/endpoint/responses.rs#L160) starts its SSE idle timer after response headers arrive. The [HTTP client builder](https://github.com/openai/codex/blob/rust-v0.153.1/codex-rs/http-client/src/client_builder.rs#L276) adds no total timeout. This supports waiting before HTTP response headers beyond the measured seven minutes without consuming the SSE idle budget. It is a source-based inference, not an hours-long empirical claim.

The default OpenAI automatic compaction path uses [remote compaction V2](https://github.com/openai/codex/blob/rust-v0.153.1/codex-rs/core/src/compact_remote_v2.rs#L386), which calls the ordinary streaming client. The isolated client reported `remote_compaction_v2 = true` and `token_budget = false`. The automatic compaction run above exercised this path across an account rejection. The older unary compaction request has an explicit 20-minute timeout with default settings; it is not the default path exercised here. This phase does not change either client implementation.

## Isolated supervised updates

These commands exercise the deployment path; they are not an acceptance claim. Run them from this checkout, in one shell, with a fresh private rig directory. Keep the source and deployment script unchanged while a deployment runs.

```sh
export CODEX_SWITCH_RIG_ROOT="$PWD/.project-private/supervised-continuity"
scripts/dev/continuity-rig prepare
scripts/dev/continuity-rig build
scripts/dev/continuity-rig start-supervised
# Build the candidate after making the intended source change.
scripts/dev/continuity-rig build-candidate
shasum -a 256 "$CODEX_SWITCH_RIG_ROOT/codex-switch" "$CODEX_SWITCH_RIG_ROOT/candidate" \
  > "$CODEX_SWITCH_RIG_ROOT/logs/binaries-before.txt"
```

`start-supervised` creates a private launchd job named `dev.codex-switch.rig.<root-path-hash>`, using `supervisor.plist` inside this rig. Its stable executable is the rig's `codex-switch`. Both launchd and the bridge inherit the same private upstream address: launchd receives it in the plist, and `deploy`/`rollback` read `upstream-origin`. This keeps every generation inside the gateway. Detached bridges use their own process session, closed standard input and `data/deployment-bridge.log` for standard output, so the deployment caller can exit without owning their lifetime or output pipes. Every generation, bridge included, appends its log lines to `data/proxy.log`. Python 3 is supplied by the project's devenv environment. The private job uses `ExitTimeOut = 0` so launchd does not kill admitted work after a shutdown deadline. `stop` removes that job, signals all processes running this rig's exact stable path, waits for them to exit, then stops the gateway. This includes a bridge retained after failed deployment. No installed proxy, menu or shared daemon is adopted or changed.

`build-candidate` writes `candidate` without replacing the running executable. The normal `scripts/dev/build` entry point and both rig build commands use `scripts/dev/build-proxy`, which embeds the checkout revision and modified state explicitly. Use a retained baseline built before the intended change when proving artifact replacement. Two builds from an unchanged checkout may have the same identity. Record executable SHA-256 hashes outside the binaries, alongside the process/revision status; a revision alone does not identify changes in a modified checkout.

Start an official WebSocket goal with a two-minute tool operation:

```sh
scripts/dev/continuity-goal.py --objective \
  'Use a tool to append exactly one START line to {effect_file}, then sleep 120 seconds in that same tool operation. Afterwards read input.txt, compute its sum, write goal-result.txt, verify the files and complete the goal. Never append START again.' \
  > "$CODEX_SWITCH_RIG_ROOT/logs/deploy-goal.txt" 2>&1 &
rig_goal_pid=$!
```

Before continuing, confirm `logs/goal-events.jsonl` contains the running sleep operation and `logs/upstream.log` records HTTP 101 for `/backend-api/codex/responses`. The sleep keeps the task and its existing WebSocket alive; it does not manufacture a long model response. Apply `http-only` only after that connection exists. Existing WebSockets remain open; subsequent upgrades receive 426 and fall back to HTTP.

```sh
cat > "$CODEX_SWITCH_RIG_ROOT/control" <<'CONTROL'
http-only
hold-body /backend-api/codex/responses
CONTROL
scripts/dev/continuity-rig exec \
  'Read input.txt with a tool, calculate its sum, write result.txt and verify it.' \
  > "$CODEX_SWITCH_RIG_ROOT/logs/deploy-sse.jsonl" 2>&1 &
rig_sse_pid=$!
```

Wait for `event=response-body-held` with `/backend-api/codex/responses` in `logs/upstream.log`. The gateway forwards one real body byte before holding the remainder. This proves an admitted response exists; merely opening a TCP connection does not. Now add a held ordinary HTTP request, after the CLI has completed its startup metadata reads:

```sh
cat > "$CODEX_SWITCH_RIG_ROOT/control" <<'CONTROL'
http-only
hold-body /backend-api/codex/responses
hold-body /backend-api/codex/models
CONTROL
rig_base="http://$(cat "$CODEX_SWITCH_RIG_ROOT/listen")/client/$(cat "$CODEX_SWITCH_RIG_ROOT/data/client-capability")"
curl --fail --silent --show-error \
  "$rig_base/backend-api/codex/models?client_version=0.153.1" \
  > "$CODEX_SWITCH_RIG_ROOT/logs/deploy-models.json" &
rig_http_pid=$!
curl --fail --silent "$rig_base/codex-switch/deploy-status" \
  > "$CODEX_SWITCH_RIG_ROOT/logs/deploy-before.json"
```

Wait for the models request's `event=response-body-held`. Both held bodies contain real upstream data; only their delivery timing is simulated. Then start the actual deployment without waiting for the old generation to finish:

```sh
scripts/dev/continuity-rig deploy "$CODEX_SWITCH_RIG_ROOT/candidate" \
  > "$CODEX_SWITCH_RIG_ROOT/logs/deploy-command.txt" 2>&1 &
rig_deploy_pid=$!
curl --fail --silent "$rig_base/codex-switch/deploy-status" \
  > "$CODEX_SWITCH_RIG_ROOT/logs/deploy-during.json"
```

Repeat the status read until it identifies a different PID with the candidate revision. Check the original process still exists and both held responses remain incomplete. The deployment command should still be waiting for admitted work to drain. Status identifies a generation; a fresh request through that generation is also required. Release only the body holds, leaving HTTP fallback active, then run fresh official work through the current listener:

```sh
printf 'http-only\n' > "$CODEX_SWITCH_RIG_ROOT/control"
scripts/dev/continuity-rig exec \
  'Read input.txt using a tool and report its sum without modifying any files.' \
  > "$CODEX_SWITCH_RIG_ROOT/logs/deploy-new-work.jsonl" 2>&1
wait "$rig_http_pid"
wait "$rig_sse_pid"
wait "$rig_goal_pid"
wait "$rig_deploy_pid"
curl --fail --silent "$rig_base/codex-switch/deploy-status" \
  > "$CODEX_SWITCH_RIG_ROOT/logs/deploy-after.json"
shasum -a 256 "$CODEX_SWITCH_RIG_ROOT/codex-switch" "$CODEX_SWITCH_RIG_ROOT/candidate" \
  > "$CODEX_SWITCH_RIG_ROOT/logs/binaries-after.txt"
```

Verify the models JSON is complete, both official tasks finished, the goal result matches the input sum and its effect file contains one `START`. Check the gateway log has releases rather than cancellations for both holds. The final status must identify the supervised successor, distinct from the old process and bridge; the deployment log names that successor. Compare completed-response records and task IDs when checking for replay. Keep command exit statuses and logs, not just a screenshot of the final status.

Repeat the overlap for rollback, starting with an empty control file so the new goal can establish its WebSocket. Use `scripts/dev/continuity-rig rollback` in place of the background `deploy` command. It installs the saved `.previous` executable through the same bridge/drain path. After all work finishes, stop only the isolated rig:

```sh
scripts/dev/continuity-rig stop
```

`scripts/dev/deploy-readiness-proof` is a separate **simulated control-plane proof**. It invokes the actual deployment script with disposable processes and simulated status responses, including empty and wrong-revision successors. It does not exercise launchd, network handover or model traffic. The supervised procedure above supplies those operating observations; neither substitutes for the other. Live proxy adoption still needs its own explicit approval.

### Failed startup and successor

A candidate that exits unsuccessfully from `print-revision` must leave the stable executable and `.previous` unchanged. A candidate that reports a revision but exits from `serve` must fail bridge readiness, restore the stable executable and retain `.previous`. Check both file hashes and the old serving PID after each failure, then run official client work. These are executable-startup simulations against the real supervised proxy.

To exercise actual launchd successor failure, start another long WebSocket goal and deploy the candidate in the background as above. Once the bridge answers with the candidate revision and a new PID, replace only this private rig's stable inode with a deliberately failing executable:

```sh
cat > "$CODEX_SWITCH_RIG_ROOT/simulated-startup-failure" <<'FAILURE'
#!/bin/sh
exit 1
FAILURE
chmod +x "$CODEX_SWITCH_RIG_ROOT/simulated-startup-failure"
mv "$CODEX_SWITCH_RIG_ROOT/simulated-startup-failure" "$CODEX_SWITCH_RIG_ROOT/codex-switch"
```

The existing processes keep their executable inodes. Let the old goal finish naturally. Launchd will attempt the failing stable path; deployment must return failure after its successor polling window, leaving the real bridge serving. Check the private job's failed launches with `launchctl print` for its exact label. Run a fresh official goal through the bridge after deployment has failed, and verify its result and single append effect.

Restore the private stable executable by inode swap, let launchd start it, and check that the successor answers with the candidate revision:

```sh
cp "$CODEX_SWITCH_RIG_ROOT/candidate" "$CODEX_SWITCH_RIG_ROOT/codex-switch.next"
mv "$CODEX_SWITCH_RIG_ROOT/codex-switch.next" "$CODEX_SWITCH_RIG_ROOT/codex-switch"
# After the goals finish and the successor is confirmed:
scripts/dev/continuity-rig stop
```

This simulation changes no provider responses and does not restart any goal. Keep the old, bridge and successor status records, gateway admissions, client events and executable hashes with the private proof logs.

### Observed deployment results, 5 September

Forward update and rollback ran under the private launchd job with distinct executable hashes. In each direction the old process retained an official Codex WebSocket, a partially delivered real Responses SSE body and a partially delivered real models HTTP body. Gateway connection addresses matched the old process's sockets. Both holds released with no cancellations, both goals completed with one append effect each, and new official work reached the replacement. Launchd's successor then replaced the bridge. The forward WebSocket and HTTP goals completed in 155 and 63 seconds; rollback completed in 124 and 65 seconds.

Real failed-artifact probes confirmed that preflight failure leaves the stable file unchanged and failed bridge startup preserves both stable and rollback files. A real failed-supervisor experiment initially exposed a bridge lifetime defect: command exit removed the bridge's process group. The corrected detached launch survived command exit, including a plain invocation without caller output redirection. The old goal completed in 72 seconds; a fresh goal started after deployment returned failure completed through that same retained bridge in 25 seconds, with one append effect and the correct sum. Launchd's actual failed spawns were recorded.

After restoring the private stable inode, launchd started the expected executable. Cleanup removed the private job and waited for both the supervised process and retained bridge before stopping the gateway. All implementer test processes are stopped. Evidence is retained under `.project-private/deployment-rig/logs/`, including goal events, gateway admissions, process/socket records, artifact hashes and `cleanup-order.json`. Exhaustion, body delays and failed executable starts are labelled simulation; model responses and tools were real. Manager acceptance and cold review remain separate gates.
