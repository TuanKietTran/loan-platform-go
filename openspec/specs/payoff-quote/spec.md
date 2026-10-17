# Payoff quote

## Purpose

Quote the amount required to pay the loan: outstanding principal plus accrued interest.

## Requirements

### Requirement: Quote principal plus accrued interest
The system SHALL return the outstanding principal, the accrued interest, and their sum.

#### Scenario: New loan
- **GIVEN** a loan was created with principal 1000 and no accrual has been applied
- **WHEN** a client gets `/v1/loans/{id}/payoff`
- **THEN** the response is HTTP 200
- **AND** `Principal` is 1000
- **AND** `AccruedInterest` is 0
- **AND** `Total` is 1000

### Requirement: Unknown loan
The system SHALL respond that a missing id was not found.

#### Scenario: Unknown id
- **WHEN** the id is a UUID that was never created
- **THEN** the response is HTTP 404 and the body contains `loan not found`

### Requirement: Invalid id
The system SHALL reject an id that is not a UUID.

#### Scenario: Not a UUID
- **WHEN** the path id is not a UUID
- **THEN** the response is HTTP 400 and the body contains `invalid id`
