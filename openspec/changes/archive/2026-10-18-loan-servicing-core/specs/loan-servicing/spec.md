## ADDED Requirements

### Requirement: Synthetic application underwriting
The pure agreement domain MUST validate a borrower UUID, distinct funding/borrower account UUIDs, USD scale 2 with exact minor-unit principal, UTC start date, positive income and nonnegative debt. It MUST generate an annuity or straight-line schedule and apply the 40% affordability ceiling to its largest instalment. Fraud ALLOW produces PENDING, REVIEW parks the application, DENY/BLOCK or unaffordability produces DECLINED. Unknown fraud decisions MUST fail closed.

#### Scenario: A parked application cannot disburse
- **GIVEN** an affordable application scored REVIEW
- **WHEN** disbursement is attempted
- **THEN** it is rejected without changing the agreement.

### Requirement: Payment evidence before cash application
Disbursement and repayment MUST require a SETTLED payment UUID with a nonempty ledger transaction UUID, exact amount and currency, and matching account direction. An agreement MUST NOT apply the same payment UUID twice. These domain checks verify the shape and consistency of evidence, not its remote authenticity; only trusted payment adapters may supply it.

#### Scenario: No success before ledger evidence
- **GIVEN** a pending approved agreement
- **WHEN** a payment lacks a ledger reference or is still PROCESSING
- **THEN** the loan remains pending with no outstanding principal.

### Requirement: Dated servicing and waterfall
Active or delinquent agreements MUST accrue ACT/365F interest from UTC calendar dates, reject backwards dates, and carry cumulative principal-days so splitting an accrual period cannot cause repeated cent rounding. Payoff quotes MUST include outstanding principal, accrued interest, and unpaid fees without mutating the agreement. Late fees MUST be assessed once per overdue unpaid instalment, beginning the day after the due date, with no grace period. Assessment is explicit and reevaluates delinquency from cumulative scheduled payments; fees are excluded from payment progress.

#### Scenario: Leap-day daily and batched accrual agree
- **GIVEN** USD 1200 at 12% starting 2024-01-01
- **WHEN** 60 days accrue, either once or daily
- **THEN** accrued interest is USD 23.67 in both cases.

#### Scenario: Repayment allocation
- **GIVEN** an overdue loan with USD 10 fees and USD 12.62 accrued interest
- **WHEN** a settled USD 30 repayment is applied
- **THEN** it pays USD 10 fees, USD 12.62 interest, and USD 7.38 principal.

### Requirement: Partial prepayment schedule regeneration
Repayments MUST allocate fees then accrued interest then principal. Amounts exceeding payoff, nonpositive amounts, fractional cents and invalid evidence MUST leave state unchanged. A partial prepayment reducing principal MUST regenerate the remaining schedule from the prepayment date, retaining the rate and count of still-future instalments. This resets the due-date anchor and does not promise the original maturity date. After maturity, one final monthly instalment is used. Full repayment MUST transition to PAID_OFF and prevent further accrual.

#### Scenario: Cash conservation
- **GIVEN** any positive cent-denominated principal and zero interest
- **WHEN** a partial or full settled payment is applied
- **THEN** allocation sums to the cash amount and remaining principal plus principal paid equals the original principal.
