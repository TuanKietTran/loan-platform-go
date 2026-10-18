# Loan platform

CQRS loans. Callers originate a pending loan and read a payoff quote of outstanding principal plus accrued interest. Accrual is on the repository only.

## Stack

Go 1.22, Chi, `shopspring/decimal`, Google UUID. In-memory only.

## Public API

- `POST /v1/loans`
- `GET /v1/loans/{id}/payoff`

## Checkpoints

| Date | Change | Evidence |
| --- | --- | --- |
| 2026-10-17 | Baseline spec recorded from the shipped service | `13bf845`. No handler tests and no Postgres repository yet. |
| 2026-10-17 | 1000 requests in one minute, retry, playback, coverage | `internal/playback`. 1000 keyed loans plus one 503 retry each finished in 80ms. Replay returned the original ids. A missing key still creates another loan. Statement coverage 61.6%. |
| 2026-10-17 | Definition of done and playback storyboard | `openspec/dod.md` and `openspec/gifs/playback-flow.gif`. GIF review: retry stays at the pre-write state, playback shows the unchanged result. |
| 2026-10-18 | Pure agreement servicing core | `e417e73`; `GOTOOLCHAIN=local go test ./internal/domain -count=1 -cover` -> 97.4%. No runtime wiring claim. |

### 2026-10-18 servicing-core checkpoint

Pure application/agreement servicing is implemented in `internal/domain/agreement.go` and asserted by `agreement_test.go`. This is not yet wired into HTTP or persistence. Evidence: `GOTOOLCHAIN=local go test ./internal/domain -count=1 -coverprofile=/tmp/loans-domain-cover.out` reports 97.4% domain statement coverage on macOS arm64, Go 1.27.1. The checkpoint commit is `e417e73`. The archived change is `2026-10-18-loan-servicing-core`.

### Atomic agreement store checkpoint

`internal/repository/agreement.go` and `internal/command/agreement.go` provide process-local atomic application/servicing commands with original-response replay, payload conflicts and injectable fraud screening/IDs. `go test ./internal/command ./internal/repository -race -count=1 -cover` proves concurrency and cancellation. Archived change: `2026-10-18-atomic-agreement-store`. Commit identity is recorded in STATUS and the next checkpoint row; no SQL or remote adapter claim.

## Out of scope until implemented

- `RecordRepayment`, `AccrueInterest` as a command, `GetLoan`, and `GetSchedule` are named in the README and have no HTTP route.
- `InMemoryLoan.AccrueInterest` exists and is not called by the server.
- `migrations/001_init.sql` is not used by the server. The `loans.borrower_id` column has no value on the create command.

## Definition of done

See `openspec/dod.md`. The playback storyboard is `openspec/gifs/playback-flow.gif`.
