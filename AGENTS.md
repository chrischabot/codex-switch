# Codex Switch product rules

Codex Switch has one purpose: keep Codex usable across multiple ChatGPT accounts, prove that work is balanced between them, and update without interrupting running agents.

## Product integrity

- Make the product coherent and immediately understandable without explanatory copy.
- Never expose uncertainty as UI. Do not add caveats, warnings, notices, information bars, ranges, minimum or maximum values, approximations, remainders, methodology, or hedging because an underlying mechanic is not understood.
- Treat uncertain UI as evidence that the product is off track. Establish the source of truth or remove the unsupported feature.
- Preserve the compact menu direction: a clear working state, Today / 7 Days / 30 Days, an account-split donut, compact allowance cards, real routed tasks, a privacy control, a footer, and monitor-aware scrolling.
- Do not replace missing or incorrect data with prose. Fix the data path.

## Live safety

- Never interrupt a running agent.
- Treat the source tree, installed menu app, and live proxy as separate states until their identity is proved.
- Do not install, restart, kill, unload, switch, or reconfigure a live proxy or menu app without explicit approval for that exact action.
- Prove proxy changes in an isolated environment. Keep proxy deployment separate from menu-app deployment.
- A safe proxy update admits new work on the new process while every admitted HTTP, SSE, and WebSocket request finishes on the old process. Never replay model work.

## Engineering

- Prefer simple, explicit designs with one obvious path. Apply KISS, YAGNI, the Zen of Python, strong module boundaries, and descriptive searchable names.
- This is a greenfield product. Remove obsolete designs instead of preserving them through compatibility code.
- Keep one canonical repository. Do not create a separate export repository.
- Prefer strongly typed contracts. Do not add a stringly typed JSON boundary when protobuf or an existing typed boundary can express the contract.
- Keep code as boring plumbing. Resolve product meaning and uncertain source interpretation from evidence rather than adding state machines, flags, or defensive branches.
- Prove the operating path before adding guardrails, regression protection, compatibility machinery, or tests.

## Proof and tests

- Test what a person uses: the real CLI, proxy process, Codex traffic, signed menu app, rendered UI, and isolated deployment path.
- Unit tests, synthetic fixtures, schemas, line counts, screenshot dimensions, and self-agreeing fakes are supporting evidence, not product acceptance.
- For Mac UI work, open the actual app with Computer Use, inspect every relevant state on screen, and compare it with the applicable design screenshots. A successful build is not proof.
- Keep a test only when it protects a real human path, a hard external contract, or a small critical rule.

## Work structure

- The lead agent owns product judgement and integration. Delegate bounded, orthogonal outcomes with explicit file boundaries.
- Recover the human intent and accepted or rejected design screenshots before changing product behaviour.
- Review each coherent slice from zero context for simplicity, naming, mission alignment, human-path correctness, and value to the product mission.
- Do not substitute reviews, plans, ledgers, tests, or code deletion for a working product.
