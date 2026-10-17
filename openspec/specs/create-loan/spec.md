# Create loan

## Purpose

Store a pending loan whose outstanding principal starts equal to the principal and whose accrued interest starts at zero.

## Requirements

### Requirement: Originate a pending loan
The system SHALL store the posted principal, interest rate, and term, and return a loan id.

#### Scenario: Create
- **WHEN** a client posts `principal` 1000, `interest_rate` 0.12, and `term_months` 12
- **THEN** the response is HTTP 200 and contains `loan_id`
- **AND** the loan status is `PENDING`
- **AND** outstanding principal is 1000
- **AND** accrued interest is 0

### Requirement: No origination checks
The system SHALL accept the numbers it is given, including zero or negative principal, rate, or term.

#### Scenario: Zero principal
- **WHEN** the principal is 0
- **THEN** the response is HTTP 200 and a loan id is returned

### Requirement: Optional idempotency key
The system SHALL originate a loan at most once when `idempotency_key` is set, and SHALL include the original loan id in the rejection. A create that omits the key SHALL originate another loan.

#### Scenario: Keyed create is not repeated
- **GIVEN** a create with key `l-1` returned loan id L
- **WHEN** the same body is posted again
- **THEN** the response is HTTP 400
- **AND** the body contains `idempotency key already used` and L
- **AND** a payoff read for L is unchanged

#### Scenario: Missing key creates another loan
- **WHEN** two creates omit `idempotency_key`
- **THEN** both responses are HTTP 200
- **AND** the loan ids differ

### Requirement: Reject invalid JSON
The system SHALL reject a body that is not JSON.

#### Scenario: Not JSON
- **WHEN** the body is not JSON
- **THEN** the response is HTTP 400 and the body contains `invalid json`
