# Project Context

Start with [llm-wiki](wiki/index.md) and its current-state page when
resuming work without conversation history. Read topic pages as needed rather
than loading the entire repository. The wiki is a navigation aid, not a substitute
for inspecting current code and git status.

Use the installed `karpathy-llm-wiki` skill for wiki ingest, query and lint.
Its SKILL.md is in the user's Codex skills directory. If unavailable, disclose
that and follow the documented raw/wiki structure rather than claiming use.
Immutable sources live in raw/; maintained knowledge lives in wiki/.
docs/llm-wiki is the superseded historical handoff, not a second maintained wiki.

Keep the relevant wiki pages current when changing architecture, commands,
persistence, security boundaries or verification status. Link claims to source
files. Distinguish implemented behavior, verified behavior and proposed work.

Do not put API keys, user configuration contents or private conversations in
documentation. Live API runs need explicit authorization and a current budget;
historical test reports do not authorize new requests. Preserve existing local
changes and do not commit, push or retag unless requested.
