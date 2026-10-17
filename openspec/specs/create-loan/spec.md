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

### Requirement: Reject invalid JSON
The system SHALL reject a body that is not JSON.

#### Scenario: Not JSON
- **WHEN** the body is not JSON
- **THEN** the response is HTTP 400 and the body contains `invalid json`
