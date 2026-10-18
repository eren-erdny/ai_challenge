# AI release preparation

The release assistant automates a real DeepSeek Client release workflow: collect
immutable Git changes, retrieve changed source/documentation evidence, run checks
in an exported candidate snapshot and draft user-facing release materials with AI.

From the repository root:

```powershell
.\Start-Release-Assistant.ps1 -DryRun
.\Start-Release-Assistant.ps1 -BudgetUsd 0.20 -Verify
```

Without `-Verify`, the existing TUI accepts the printed release goal after checks.
`-DryRun` needs no credentials and never calls a model. `-BaseRef`, `-HeadRef` and
`-ReleaseVersion` select the candidate; a version label does not create a Git tag.
Defaults demonstrate `w6-hw5 → w7-hw4` as `week-7-preview`.

Real results include release-notes.md, changelog-draft.md, readiness-report.md,
input/diff, check logs, model completion, citation-validated draft and file hashes.
The model has no tools and does not determine test outcomes. The application
requires human review even when every listed check passes. Failed/missing checks
block readiness. No commit, tag, push or publication is performed by the pipeline.

See [instructions, evidence and limitations](../../../examples/release-assistant/README.md).
