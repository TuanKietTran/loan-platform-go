## ADDED Requirements

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
