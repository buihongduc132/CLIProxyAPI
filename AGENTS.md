# Repository Guidelines

## Project Structure & Module Organization
- `cmd/server`: entrypoint wiring config, HTTP routes, and server startup.
- `internal`: core proxy logic (config, routing, provider clients).
- `sdk`: reusable Go client modules (`api`, `auth`, `translator`, etc.).
- `docs` and `examples`: integration guides and runnable samples.
- `config.example.yaml`: reference configuration; copy to `config.yaml` for local runs.
- `test`: Go test suite (config migration coverage).

## Build, Test, and Development Commands
- `go build ./...`: compile all modules.
- `go test ./...`: run unit tests (add `-race` for data races when feasible).
- `go run ./cmd/server`: start the proxy using `config.yaml` in the working directory.
- `docker-compose up -d`: launch via Docker with defaults from `docker-compose.yml`.
- `./docker-build.sh` (or `docker-build.ps1` on Windows): produce container image.

## Coding Style & Naming Conventions
- Go 1.24; run `gofmt` on all Go sources before commit.
- Prefer small, cohesive packages; keep files under ~300 lines.
- Use descriptive names for providers/models; keep exported symbols minimal.
- YAML keys follow kebab-case (e.g., `allow-remote`, `api-key-entries`).

## Testing Guidelines
- Tests live alongside packages or in `test/`; name files `*_test.go`.
- Cover config migrations and routing edge cases; add regression tests for bug fixes.
- For config-dependent logic, include table-driven cases with temporary files.
- CI expectation: `go test ./...` must pass; include `-race` locally when practical.

## Commit & Pull Request Guidelines
- Follow conventional commit prefixes (`feat`, `fix`, `refactor`, `chore`, `docs`, etc.).
- Keep commits small and focused; include brief rationale in the body if non-obvious.
- PRs should state scope, testing performed, and any config or API impacts; attach sample commands or screenshots when behavior changes.
- Ensure branches are synced with `main` before opening or merging.

## Security & Configuration Tips
- Never commit secrets; use environment variables or local `config.yaml`.
- Set `remote-management.allow-remote: false` unless you explicitly need external access.
- Restrict Amp management to localhost when enabled; rotate API keys used for OAuth flows.
- When adding providers, prefer least-privilege API keys and document required headers/proxies in `config.example.yaml`.
