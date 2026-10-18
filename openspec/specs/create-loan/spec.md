# Create loan

## Purpose
Store a validated keyed pending loan, returning its canonical persisted ID even across concurrent handlers. These legacy endpoints do not perform application underwriting or downstream disbursement.

## Requirements

### Requirement: Originate a pending loan
The system SHALL store positive principal, nonnegative interest rate and term 1..600, returning a canonical persisted loan ID. A new loan SHALL be PENDING, with outstanding principal equal to principal and zero accrued interest.

#### Scenario: Create
- **GIVEN** a nonblank command key of at most 200 bytes
- **WHEN** a client posts principal 1000, rate 0.12 and term 12
- **THEN** HTTP 200 returns the persisted loan ID with PENDING status.

### Requirement: Atomic canonical identity
The command SHALL require a nonblank key of at most 200 bytes. Header/body keys MUST agree when both are supplied. Identical payloads with the same key SHALL return the original persisted ID with HTTP 200; changed principal, rate or term SHALL return HTTP 409. All writers sharing a repository SHALL arbitrate atomically, not by handler-local locks.

#### Scenario: Multiple handler instances
- **GIVEN** concurrent handlers sharing one repository
- **WHEN** they create the same keyed payload
- **THEN** all return the same persisted ID and no replay resets servicing state.

### Requirement: Validate legacy terms
Nonpositive principal, negative annual rate or term outside 1..600 MUST be rejected with HTTP 400 before persistence. Previously stored invalid rows are not migrated; invalid historical payloads cannot be replayed through the command.

#### Scenario: Zero principal rejected
- **WHEN** a keyed request specifies zero principal
- **THEN** HTTP 400 is returned and no loan is persisted.

### Requirement: Reject invalid JSON
The system SHALL reject a body that is not JSON.

#### Scenario: Not JSON
- **WHEN** the body is not JSON
- **THEN** HTTP 400 is returned.
