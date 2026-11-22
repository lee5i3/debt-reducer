# Developing Debt Reducer

Debt Reducer is a Go web app (server-rendered HTML) wrapped in an Electron
desktop shell. The Go server does all the work; the desktop app starts it on
a private local port and shows its pages in a window.

## Stack

Go · [chi](https://github.com/go-chi/chi) router · [dig](https://github.com/uber-go/dig) DI ·
[templ](https://templ.guide) views · SQLite via [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite)
(pure Go, no CGO) · [Electron](https://www.electronjs.org/) +
[electron-builder](https://www.electron.build/) for the desktop app

## Getting started

```sh
make init   # download deps, create data/, generate templ code
make dev    # web app with live reload at http://localhost:7331 (app on :8080)
```

| Command              | What it does                                                   |
|----------------------|----------------------------------------------------------------|
| `make test`          | Go tests, plus the desktop app's tests when Node is installed  |
| `make lint`          | templ format check and `go vet`                                |
| `make build`         | build the server to `bin/debt-reducer`                         |
| `make run`           | build and run the web app on :8080                             |
| `make restart`       | rebuild and restart the web app in the background              |
| `make desktop`       | build and open the desktop app                                 |
| `make desktop-bins`  | cross-compile the server for every desktop platform            |
| `make desktop-dist`  | package installers into `bin/dist/` (`PLATFORMS`, `ARCHS`)     |
| `make db-reset`      | delete the local database                                      |

Run `make` to list them all.

When running Electron from a VS Code terminal, unset `ELECTRON_RUN_AS_NODE`
(VS Code sets it, and it makes Electron act as plain Node). `make desktop`
does this for you.

## Configuration

| Flag               | Env var             | Default                                                |
|--------------------|---------------------|--------------------------------------------------------|
| `-addr`            | `DEBT_REDUCER_ADDR` | `:8080`                                                |
| `-db`              | `DEBT_REDUCER_DB`   | `data/debt-reducer.db`                                 |
| `-exit-with-stdin` |                     | off (stop when stdin closes; used by the desktop app)  |

The database is created and migrated on startup. `data/` is git-ignored.

The desktop app passes `-db` itself: the folder chosen under Settings › Data
(kept in `config.json` in Electron's user data folder), or that user data
folder by default. `DEBT_REDUCER_DB` overrides it, which is handy for testing
with a scratch database.

## Versions

Versions come from git tags (`v1.2.3`) and commit messages. On every build,
CI ([github-tag-action](https://github.com/anothrNick/github-tag-action), in
dry-run mode) takes the latest `v*` tag and adds one bump based on the
commits since:

| Commit message contains            | Bump  | Example         |
|------------------------------------|-------|-----------------|
| `BREAKING CHANGE` (title or body)  | major | 0.1.0 → 1.0.0   |
| `feat` (e.g. `feat: add search`)   | minor | 0.1.0 → 0.2.0   |
| anything else (`fix:`, `docs:` …)  | patch | 0.1.0 → 0.1.1   |

It's a plain text match, so avoid "feat" in other words (like "feature") in
fix commits. Several commits still bump only once (the biggest wins) until
the release is published, which creates the tag. With no tags yet the first
version is 0.0.1; to start at 0.1.0 instead, change the draft's tag to
`v0.1.0` when publishing it. With squash merges the PR title becomes the
commit message.

The build stamps the version into the server (shown in the footer) and into
the installers. Local builds use `git describe` (e.g. `0.1.0-3-gabc1234`), or
`0.0.0-dev` before the first tag; `make version` prints it.

## CI and releases

- `.github/workflows/build.yml` (reusable): works out the version, runs
  `make lint test` and `make security`, then builds the Linux AppImage,
  Windows NSIS setup `.exe` and macOS `.dmg` (Apple Silicon and Intel) as
  artifacts (kept 14 days).
- `pr.yml` runs it on every pull request.
- `release.yml` runs it on every merge to `main`, then puts the installers on
  a **draft** release
  `v<version>`. Review and publish it on the Releases page; publishing
  creates the tag. Merging again before publishing updates the draft (or
  replaces it with a newer version if a `feat:` or breaking change came in).

`make security` checks for known vulnerabilities: `govulncheck` for Go and
`npm audit` for the desktop app's packages.

### Signing

The installers aren't code-signed yet. Windows SmartScreen warns when the
setup runs, and macOS (ad hoc signature only, no Apple Developer ID) warns
that the developer can't be verified.

## Layout

```
cmd/debt-reducer/       entrypoint: flags, dig container, graceful shutdown
internal/config/        flags / env
internal/container/     dig providers
internal/debt/          domain model + payoff calculator (pure, unit tested)
internal/storage/       SQLite connection + embedded SQL migrations
internal/repository/    debts, settings and plan persistence
internal/service/       debt management, plan building and tracking
internal/handler/       HTTP handlers (UI pages + JSON API)
internal/server/        chi router and http.Server
internal/version/       app name and version
web/views/              templ components and pages
web/static/             CSS and icons (embedded)
desktop/                Electron shell: starts the server, menu, data folder
```

## JSON API

| Method | Path                | Notes                                         |
|--------|---------------------|-----------------------------------------------|
| GET    | `/api/debts`        |                                               |
| POST   | `/api/debts`        | `{"name","balance","apr","minimumPayment","dueDay"}` |
| GET    | `/api/debts/{id}`   |                                               |
| PUT    | `/api/debts/{id}`   |                                               |
| DELETE | `/api/debts/{id}`   |                                               |
| GET    | `/api/settings`     |                                               |
| PUT    | `/api/settings`     | `{"monthlyBudget","strategy"}`                |
| GET    | `/api/preferences`  | theme, open the current month, banner time    |
| PUT    | `/api/preferences`  | `{"theme","expandCurrent","noticeSeconds"}`   |
| GET    | `/api/plan`         | `?strategy=&budget=&start=YYYY-MM-DD` (optional overrides) |
| GET    | `/api/compare`      | all strategies, schedules omitted             |
| GET    | `/api/lump-sum`     | `?amount=5000` plus optional `strategy`, `budget`, `start` (payment date) |

Amounts are dollars in JSON; they're stored as integer cents.

## Calculation notes

- Interest accrues monthly at APR / 12 on the current balance, rounded to the cent.
- The first payment for each debt is its next due date on or after the start
  date; due days past month end (e.g. the 31st) fall on the last day of the month.
- Plans are capped at 50 years; if minimums don't cover interest you'll get an error.
- Marked months (Paid/Skipped) are history: they keep the payment, interest
  and minimum recorded when they were marked, and never gain a debt added or
  reactivated later.
