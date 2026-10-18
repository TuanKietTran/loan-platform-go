# Tasks

- [x] Define agreement repository port and atomic process-local memory store.
- [x] Persist original creation and mutation replay snapshots with payload conflicts.
- [x] Clone state on every boundary and roll back failed/canceled mutations.
- [x] Implement validation-first application command with replayable screening identity.
- [x] Add servicing commands for trusted payment evidence, accrual and assessment.
- [x] Prove concurrent create/mutation replay, failure atomicity, cancellation and copy isolation with race tests.

Remote adapters, outbox, HTTP routes and SQL parity remain mandatory follow-up cards in production-loans.
