# Loan platform

CQRS loans. Callers originate a pending loan and read a payoff quote of outstanding principal plus accrued interest. Accrual is on the repository only.

## Stack

Go 1.22, Chi, `shopspring/decimal`, Google UUID. In-memory only.

## Public API

- `POST /v1/loans`
- `GET /v1/loans/{id}/payoff`

## Out of scope until implemented

- `RecordRepayment`, `AccrueInterest` as a command, `GetLoan`, and `GetSchedule` are named in the README and have no HTTP route.
- `InMemoryLoan.AccrueInterest` exists and is not called by the server.
- `migrations/001_init.sql` is not used by the server. The `loans.borrower_id` column has no value on the create command.

## Definition of done

See `openspec/dod.md`. The playback storyboard is `openspec/gifs/playback-flow.gif`.
