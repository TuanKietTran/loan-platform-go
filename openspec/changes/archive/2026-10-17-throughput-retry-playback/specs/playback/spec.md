## ADDED Requirements

### Requirement: Finish 1000 requests inside one minute
The playback client SHALL complete 1000 recorded calls in less than 60 seconds, including one transient failure and a successful retry on each call.

#### Scenario: Burst with one 503 per call
- **GIVEN** the server returns HTTP 503 on the first attempt of each distinct body and then succeeds
- **WHEN** the client plays 1000 calls with at most 3 attempts
- **THEN** every call ends on the success status
- **AND** each call took 2 attempts
- **AND** the play finishes in under 60 seconds

### Requirement: Retry only transient failures
The client SHALL retry a transport error or HTTP 408, 429, or 5xx, and SHALL NOT retry any other status.

#### Scenario: HTTP 400 is final
- **WHEN** the server returns HTTP 400
- **THEN** the client stops after 1 attempt

#### Scenario: Repeated 503 stops at the attempt limit
- **WHEN** the server returns HTTP 503 on every attempt and the limit is 2
- **THEN** the client stops after 2 attempts
- **AND** the outcome status is 503

### Requirement: Playback does not repeat the effect
Playing the same call list again SHALL NOT apply a successful idempotent write a second time. A stateless read SHALL return the same body.

#### Scenario: Second play
- **GIVEN** the first play of 1000 calls succeeded
- **WHEN** the client plays that same list again
- **THEN** stored totals are unchanged
- **AND** each outcome is either the same success body or HTTP 400 containing `idempotency key already used`
