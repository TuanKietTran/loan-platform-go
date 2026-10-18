## ADDED Requirements

### Requirement: Atomic agreement command identity
Agreement creation MUST atomically bind an application idempotency key to its payload fingerprint and original agreement snapshot. Agreement mutations MUST atomically bind (agreement ID, idempotency key) to operation/payload fingerprint, state change, and original result. A matching replay MUST return that original result; a changed fingerprint MUST conflict. Pure failed or canceled mutations MUST not commit state or reserve the key. An agreement identity MUST be immutable.

#### Scenario: Concurrent matching repayments
- **GIVEN** forty workers applying one command key and payload
- **WHEN** all complete
- **THEN** the state change is applied once and every worker sees the same original result.

#### Scenario: Mutation failure rolls back
- **GIVEN** a pure mutation that changes its private clone then returns an error
- **WHEN** the command fails
- **THEN** current stored state and command identity remain unchanged.

#### Scenario: Original snapshot after later commands
- **GIVEN** a disbursement command followed by later repayment
- **WHEN** the disbursement command replays
- **THEN** it returns the original active agreement, while reading current state returns the later serviced agreement.

### Requirement: Fail-closed application orchestration
Application commands MUST validate key and application before invoking screening. The screen event ID MUST be a deterministic UUID namespaced by the application key. Unavailable screening or an unknown decision MUST not create an agreement. Concurrent same-payload applications may invoke screening more than once using the same event identity; the screening adapter MUST enforce remote replay semantics. IDs MUST be injectable. Network calls MUST remain outside repository mutation locks.

#### Scenario: Screening unavailable
- **GIVEN** a valid new application
- **WHEN** screening returns an error
- **THEN** no application record is created and retry remains possible.

### Requirement: Honest memory-store limitation
The memory adapter MUST return isolated copies and provide process-local concurrency safety. It MUST NOT be described as durable or sufficient evidence of restart recovery.

#### Scenario: Caller modifies returned schedule
- **GIVEN** a successfully returned agreement snapshot
- **WHEN** the caller changes its schedule slice
- **THEN** subsequent store reads remain unchanged.
