# debt-reducer

A web app that calculates the fastest and cheapest way to pay off your debts.
Add each debt (balance, APR, minimum payment, due day) in the UI, set a monthly
budget, and it builds a month-by-month payment schedule from today, showing when
each debt is paid off and when you're debt free.

**Strategies**

| Strategy      | Extra money goes to…        | Best for                  |
|---------------|-----------------------------|---------------------------|
| Avalanche     | Highest APR first           | Least total interest      |
| Snowball      | Smallest balance first      | Quick wins / motivation   |
| Minimums only | Nothing extra (baseline)    | Seeing what you save      |

Any budget above the sum of minimums goes to the strategy's target debt. When a
debt is paid off, its minimum rolls into the next one.

**One-time payment** (`/lump-sum`): enter a bonus, refund or windfall and the app
tries putting it toward each debt, re-running your ongoing plan each time. Options
are ranked by interest saved, with months saved and the monthly minimum freed up
(if the payment clears a debt) shown for each. Money beyond the target's balance
flows to the next highest-APR debt.

## Stack

Go · [chi](https://github.com/go-chi/chi) router · [dig](https://github.com/uber-go/dig) DI ·
[templ](https://templ.guide) views · SQLite via [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) (pure Go, no CGO)

## Getting started

```sh
make init   # download deps, create data/, generate templ code
make dev    # live reload at http://localhost:7331 (app on :8080)
```

Other targets: `make build`, `make run`, `make test`, `make lint`, `make fmt`,
`make clean`, `make db-reset`. Run `make` to list them.

## Configuration

| Flag    | Env var             | Default                 |
|---------|---------------------|-------------------------|
| `-addr` | `DEBT_REDUCER_ADDR` | `:8080`                 |
| `-db`   | `DEBT_REDUCER_DB`   | `data/debt-reducer.db`  |

The database is created and migrated on startup. `data/` is git-ignored.

## Layout

```
cmd/debt-reducer/       entrypoint: flags, dig container, graceful shutdown
internal/config/        flags / env
internal/container/     dig providers
internal/debt/          domain model + payoff calculator (pure, unit tested)
internal/storage/       SQLite connection + embedded SQL migrations
internal/repository/    debts & settings persistence
internal/service/       debt management + plan building
internal/handler/       HTTP handlers (UI pages + JSON API)
internal/server/        chi router and http.Server
web/views/              templ components
web/static/             CSS (embedded)
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
| GET    | `/api/plan`         | `?strategy=&budget=&start=YYYY-MM-DD` (optional overrides) |
| GET    | `/api/compare`      | all strategies, schedules omitted             |
| GET    | `/api/lump-sum`     | `?amount=5000` plus optional `strategy`, `budget`, `start` (payment date) |

Amounts are dollars in JSON; they're stored as integer cents.

## Calculation notes

- Interest accrues monthly at APR / 12 on the current balance, rounded to the cent.
- The first payment for each debt is its next due date on or after the start
  date; due days past month end (e.g. the 31st) fall on the last day of the month.
- Plans are capped at 50 years; if minimums don't cover interest you'll get an error.
