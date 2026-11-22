# CLAUDE.md

Debt Reducer: a Go web app that works out the fastest and cheapest way to pay off
debts (avalanche, snowball, minimums only) and where a one-time payment does the
most good.

## Start of every session

Read these local files first. They are git-ignored, so they may not exist on a
fresh clone:

- `RESTART.md`: what was in progress and where it stopped
- `MEMORY.md`: key facts and decisions so far
- `BACKLOG.md`: everything still left to do

At the end of a work session, or before stopping mid-task, update `RESTART.md`
and `BACKLOG.md`. Add any lasting decision to `MEMORY.md`.

## Stack (fixed; don't swap without asking)

- Go, standard layout: `cmd/`, `internal/`, `web/`
- Router: chi (`github.com/go-chi/chi/v5`)
- Dependency injection: dig (`go.uber.org/dig`). Every constructor is registered in `internal/container/container.go`
- Views: templ, pinned as a Go tool (`go tool templ`); generated `*_templ.go` files are git-ignored. `web/views` is split by role:
  - `web/views/components/` (package `components`): shared building blocks: app shell/`Layout` and `Shell`, `Modal`, `Alerts`, `Icon*`, `MoneyInput`
  - `web/views/pages/` (package `pages`): one `.templ` per page (plan, simulate, debts, settings, error) plus its view models and formatting helpers
  - New reusable pieces go in `components` (exported); page-only markup stays in `pages`
- Icons: Phosphor (https://phosphoricons.com), regular weight, vendored in `web/static/vendor/phosphor` (font + CSS, MIT). Use `@components.Icon("name")` or the named `components.Icon*` wrappers; don't hand-draw SVG icons
- Storage: SQLite via `modernc.org/sqlite` (pure Go, `CGO_ENABLED=0`). Migrations are embedded SQL in `internal/storage/migrations/NNN_name.sql`
- Plain HTML forms with redirect-after-POST, no JS framework. A JSON API under `/api` mirrors the UI
- Desktop app: Electron shell in `desktop/` (`main.js`, plus the native menu in `menu.js`; no page UI of its own). Menu items load the same URLs as the sidebar (Settings adds `?settings=1` to the current page); add a menu item when adding a top-level page. It runs the Go server as a hidden helper with `-addr 127.0.0.1:0 -exit-with-stdin`, reads the port from the `msg=listening addr=` log line and loads the pages in a window, so all UI work stays in templ/app.css. Desktop data (log, window size, `config.json`) lives in Electron's userData folder (`~/.config/Debt Reducer/` on Linux); the database is there too unless moved under Settings > Data (the folder is kept in `config.json`, since the database can't hold its own location) or `DEBT_REDUCER_DB` is set. `desktop/preload.js` exposes `window.debtReducer` (choose/open data folder) to the app's pages only; the file moves live in `desktop/data.js` (tested with `node --test`, run by `make test`) The server is cross-compiled with `CGO_ENABLED=0` per platform into `bin/desktop/<os>-<arch>/` and bundled by electron-builder

## Commands

README.md is for people installing the app; developer docs go in DEVELOPMENT.md.

```sh
make init      # deps, data dir, templ generate
make dev       # live reload at http://localhost:7331 (app on :8080)
make test      # generate + go test ./... (+ desktop node tests)
make security  # govulncheck + npm audit
make lint      # templ fmt check + go vet
make build     # bin/debt-reducer
make db-reset  # delete the local database
make restart   # rebuild, stop the running app, start it again in the background
make stop      # stop whatever is serving the app port
make logs      # follow the log of the app started by make restart
make desktop   # build and open the desktop app (set DEBT_REDUCER_DB to a scratch db for testing)
make desktop-bins  # cross-compile the server for linux/windows/mac (x64, arm64)
make desktop-dist  # installers into bin/dist (PLATFORMS="--linux --win"; mac builds need a Mac)
```

## When a change is done

1. Run `make lint test`. Fix anything that fails before going on
2. Run `make restart` so the running app at http://localhost:8080 serves the new code. It replaces whatever is on the port (`go run`, `make run` or an earlier restart) and waits for `/healthz`
3. If the restart fails, read `bin/debt-reducer.log`, fix the cause and restart again. Don't leave the app stopped
4. Say in the summary that the app was restarted (or why it wasn't)

`make restart` uses the real database (`data/debt-reducer.db`), and new migrations run on it at startup. If a migration changes existing data, test it on a copy first (`-db` in the scratchpad). The database runs in WAL mode: copy `debt-reducer.db` together with its `-wal` and `-shm` files, or the copy misses recent changes.

VS Code terminals set `ELECTRON_RUN_AS_NODE=1`, which makes Electron act as plain Node. `make desktop` unsets it; do the same when running electron or electron-builder by hand.

## CI

- `.github/workflows/build.yml` (reusable, `workflow_call`): version (paulhatch/semantic-version: latest `v*` tag + Conventional Commits bump), `make lint test`, `make security`, then AppImage (Linux), NSIS setup (Windows, unsigned) and dmg (macOS arm64 + x64, ad hoc signed) as artifacts. `pr.yml` calls it on pull requests; `release.yml` calls it on pushes to main and keeps one draft release `v<version>` via `gh`. Keep every step named, a blank line between steps, and comments explaining why
- Pin actions by full commit SHA with the version in a comment (`uses: actions/checkout@<sha> # v7.0.1`), runners by OS release (`ubuntu-24.04`, `windows-2025`), Node by exact version. `.github/dependabot.yaml` updates Go, npm (`desktop/`) and actions weekly

## Code rules

- Money is `debt.Money` (int64 cents). Never use float64 for stored or summed amounts. Parse user input with `debt.ParseDollars`
- The domain logic in `internal/debt` stays pure: no database, HTTP or time.Now() inside the calculator (start date comes in on the request). Cover it with unit tests
- Layering: handler → service → repository/calculator. Handlers don't touch `*sql.DB`
- Validation errors wrap `service.ErrInvalid`. Handlers map errors to status codes in `statusFor` and never show internal error text to users
- Schema changes are new migration files only. Never edit an applied migration
- The Plan is saved and tracked (`service.PlanService`): history comes from marked months (`plan_months`, `plan_month_debts`), the rest is projected starting at the next unmarked month. A debt's stored balance is its starting amount as of its `start_date`; what's left is derived by replaying marked months from that month on (`internal/service/replay.go`). Marking Paid/Skip and Undo never change debts. A marked month keeps its recorded payment, interest and minimum (`plan_month_debts.minimum_cents`) when a debt is edited later. Marked months never gain debts: one that wasn't in a month (inactive then, reactivated or added later) stays out of it and joins the plan from the next unmarked month with the amount it had. Months are marked in order; only the latest can be undone
- Plan and Simulate need at least one active debt: their routes sit behind `Handler.RequirePlan` (redirects to `/debts`), the sidebar greys them out (`Shell.CanPlan`), and the desktop menu does the same (`refreshMenu`). New plan/simulate routes go inside that group
- The version comes from git tags: CI computes the next one from commit messages (use Conventional Commit messages: `feat:` minor, `!:`/`BREAKING CHANGE:` major, else patch) and passes `VERSION=`; locally `git describe` or `0.0.0-dev`. It's set at build time into `internal/version.Version` (and the desktop package version); the footer shows it
- Paid-off debts keep a zero balance, and inactive debts (`Debt.Inactive`) stay listed but out of the plan. Anything that runs the calculator must leave both out (the calculator rejects balance 0)
- After editing `.templ` files, run `make generate` (or `make dev`). If `templ fmt` reflows text, re-check that it kept the spacing you meant

## UI rules

- Keep the existing clean dashboard style: dark theme, system font, narrow left sidebar (name, nav, summary and Settings at the bottom; no top bar or logo), accent `#6ea0ff` with dark text on accent buttons, summary cards, bordered tables, form panels. Colors are CSS variables at the top of `web/static/app.css` (dark by default, with a `:root[data-theme="light"]` override block); use them instead of new hex values, and add any new token to both blocks
- Settings (`components.SettingsDialog`) is a large popup laid out like an editor's settings: search, a section list (Data, Appearance, Plan; one section shows at a time, search spans all) and settings named "Section: **Name**" with a description and control. Add new settings as `.setting` blocks with `data-setting` in the right section so search finds them. Desktop-only controls carry `data-desktop` (hidden until `window.debtReducer` exists), browser-only notes `data-web`
- Popups (add/edit, details, delete) are server-rendered `<dialog>`s, each with its own URL (`/debts/new`, `/debts/{id}`, `/debts/{id}/edit`, `/debts/{id}/delete`). A small script in the layout turns them into modals; Escape or a backdrop click goes back to `/debts`. Don't add a JS framework for this
- Every dollar amount input uses `@components.MoneyInput(components.MoneyField{...})` (`web/views/components/inputs.templ`): "$" add-on, decimal keyboard, value formatted as 1,234.56 on render, while typing and on blur; numbers only. Don't hand-write amount inputs
- Every page renders inside `components.Layout(shell, title)`. Handlers get the sidebar data from `Handler.shell(r, active)` and render errors with `Handler.errorPage`
- Don't restyle the whole app in one go. A bold editorial redesign was tried and rejected
- For visual changes, show a mockup first (a claude.ai Artifact canvas), get a pick, then port it into `web/views/pages` (and `components` for shared parts) and `app.css`

## Working together

- Go ahead with routine steps. Ask before anything destructive or outward-facing
- Commits are the user's decision. Don't commit, and don't ask or offer to commit; only do it when explicitly told to (then on a branch). Never commit `data/`, the local `.md` notes above, or `.env`
- Use a throwaway database (`-db` in the scratchpad) for testing. Don't put test data in `data/debt-reducer.db`
- Report results plainly: what changed, what was verified (tests, browser check), and what wasn't
