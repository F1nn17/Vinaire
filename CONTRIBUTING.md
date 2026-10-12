# Contributing

Thanks for looking. Issues and pull requests are welcome.

- **Read [AGENTS.md](./AGENTS.md) first** — it holds the architecture and the rules that are not negotiable:
  Vinaire's writes are a short, explicit, audited list, every request runs with the caller's own identity, and the
  security checklist there applies to every change. A change that adds a way to write to a cluster is a decision to
  discuss in an issue before it is coded.
- **Run the checks** before opening a pull request:

  ```bash
  cd backend  && go build ./... && go vet ./... && gofmt -l . && go test ./...
  cd frontend && npm run lint && npm run check:i18n && npm run check:theme && npx tsc --noEmit && npm run build
  ```

- **Tests live with the feature.** A change that fixes a bug adds the test that would have caught it.
- **Interface text** is English in the source and translated through `frontend/src/lib/i18n/dict/ru.json`
  (`npm run check:i18n`).
- **Commits** follow Conventional Commits (`feat:`, `fix:`, `docs:` …), and a user-visible change adds a line to the
  `[Unreleased]` section of [CHANGELOG.md](./CHANGELOG.md).
- **Security problems** are reported privately — see [SECURITY.md](./SECURITY.md).

Releases are made by the maintainer as described in [docs/releasing.md](./docs/releasing.md).
