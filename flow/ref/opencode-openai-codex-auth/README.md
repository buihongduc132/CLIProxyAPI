# Refreshing opencode Codex model list

Follow these steps to update cached model metadata and prompts when upstream Codex models change (e.g., GPT 5.2):

1) Source list
- Download `full-opencode.json` from upstream:
  `curl -L https://raw.githubusercontent.com/numman-ali/opencode-openai-codex-auth/main/config/full-opencode.json -o flow/ref/opencode-openai-codex-auth/full-opencode.json`
- Keep this file versioned for comparison and audits.

2) Purpose
- File captures the authoritative model list used by the opencode Codex auth plugin; we diff against it when adding new models (e.g., gpt-5.2) to our registry and routing.

3) How to consume updates
- After downloading, inspect model additions/renames: `jq '.provider.openai.models[]?.name // empty' flow/ref/opencode-openai-codex-auth/full-opencode.json | sort -u`.
- Mirror any new models into `internal/registry/model_definitions.go` and executor routing tables.
- If new prompt families appear (e.g., gpt-5.2), ensure `CodexInstructionsForModel` mapping covers them.

4) Caching logic alignment
- Our runtime now fetches prompts from the latest `openai/codex` release with ETag caching at `~/.opencode/cache/*-instructions.md`.
- Use this reference file to validate that cached prompts align with upstream model names (see mapping in `internal/misc/codex_instructions.go`).

5) When to repeat
- Repeat after upstream release notes mention new Codex/GPT versions or when opencode plugin bumps major/minor versions.
