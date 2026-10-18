# Definition of done

**Met** means implemented and asserted by a named test. Coverage does not establish runtime integration or durability. Measurement: 2026-10-18, macOS arm64, Go 1.27.1, `GOTOOLCHAIN=local go test -p 2 ./... -race -count=1 -coverprofile=coverage.out -cover`. Package statement coverage: API 100%, command 98.2%, domain 97.4%, query 100%, repository 100%, playback 61.5%, cmd/server 0%. Statement-weighted non-cmd: 441/467 = 94.43%. Full output/profile are retained in the lane control logs (`card4b1-gates.log`, `card4b1-cover.out`), not committed artifacts.

## Met

| Done when | Test evidence |
| --- | --- |
| Keyed legacy create replays HTTP 200 and changed payload conflicts 409 | `TestCreateRequiresKeyReplayAndConflict` |
| Concurrent handler instances return one persisted identity | `TestCreateLoanCanonicalIdentityAcrossHandlers` |
| Invalid terms/keys are rejected; header/body mismatch is 400 | `TestCreateLoanValidatesTermsAndPropagatesCancellation`, `TestLegacyValidationAndPayoffErrors` |
| Legacy payoff includes accrued interest; missing loan and invalid UUID are rejected | `TestLegacyPayoffIncludesAccruedInterestAndMissingLoan`, `TestLegacyValidationAndPayoffErrors` |
| Application affordability and supplied fraud decisions govern disbursement eligibility | `TestApplicationValidation`, `TestDisbursementRequiresMatchingSettledEvidence` |
| ACT/365F daily accrual equals batched accrual, including leap day | `TestCumulativeAccrualLeapDayAndQuote` |
| Late fee assessed once and repayment uses fee-interest-principal waterfall | `TestLateFeeOnceAndRepaymentWaterfall` |
| Partial prepayment regenerates principal-conserving schedule; full payoff closes agreement | `TestRepaymentPrepaymentAndPayoff`, `FuzzRepaymentConservesCash` |
| Memory command replay returns original snapshots, rolls back failures, and isolates copies | `TestMemoryAgreementConcurrentCreateAndMutationReplay`, `TestMemoryAgreementRollbackCancellationAndValidation` |
| Screening failures fail closed; repeated/concurrent application identity is stable | `TestAgreementServiceFailsClosedAndValidatesBeforeScoring`, `TestConcurrentApplicationUsesStableFraudIdentity` |
| Existing 1000-request synthetic retry playback remains a regression | `internal/playback` tests; in-process only, not a real-stack load result |

## Partial

| Capability | Limitation |
| --- | --- |
| Lifecycle domain and command/store | Pure agreement aggregate and process-local commands only; legacy HTTP create/payoff does not use them |
| Underwriting with fraud | Injectable screening port tested; no remote fraud adapter yet |
| Cash movement through payments | Domain requires settled payment/ledger evidence; no remote adapter/outbox yet |
| OpenAPI | Current legacy routes/statuses and payoff field casing are asserted by `TestOpenAPIImplementedRoutesAndStatuses`; future lifecycle contracts remain unavailable |

## Unmet

| Capability | Missing evidence/work |
| --- | --- |
| Runtime lifecycle HTTP and S2 | Application/read/schedule/disbursement/repayment/accrual/fee/prepayment/payoff execution routes |
| Durable store and restart recovery | Postgres adapter, migration parity tests and durable outbox |
| Downstream resilience | Fraud/payment clients, retries/jitter, circuit breaker, timeout/503/ambiguous-commit drills |
| Operations and documentation | Validated environment, health/readiness/metrics, logging, bounded HTTP bodies/timeouts, graceful shutdown, error envelope, ADRs, CI and complete README |
| Real-process 10k load | Manager-owned integration/load evidence not yet available |

The GIF in `openspec/gifs/playback-flow.gif` illustrates the old baseline replay contract. It is historical, not evidence of current response status or real-process durability. No production-ready claim is made.
