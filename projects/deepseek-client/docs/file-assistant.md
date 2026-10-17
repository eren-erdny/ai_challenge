# Project file assistant — homework 34

The file-assistant demo reuses the existing agent, TUI and confined file tools.
It binds the tools to a fresh embedded TaskDesk fixture. DeepSeek chooses its
own discovery/read/edit calls from two goal-level requests; preparation never
performs the documentation fixes.

Run from the repository root:

```powershell
.\Start-File-Assistant.ps1 -BudgetUsd 0.20
```

Add `-Verify` to execute both goals through scripted TUI submit/update. This
verification does not open a visible terminal and is not a video recording.
The ordinary mode opens the existing interactive TUI.

Every invocation preserves a new run under `.file-assistant/runs`. Runs share
a locked durable budget ledger. Writes are limited to the three requested
Markdown outputs. Original source and test files remain read-only. Real traces,
file hashes and a unified diff prove the effects independently of final model text.

The embedded fixture's documentation intentionally contradicts its code. The
fixture represents export authorization and metadata; it is not a PDF renderer.

See [the complete instructions and real results](../../../examples/file-assistant/README.md)
for goals, artifacts, pricing assumptions, offline replay and verification limits.
