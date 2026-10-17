## ADDED Requirements

### Requirement: Published loan API contract
The service SHALL publish an OpenAPI document that lists only callable paths under paths and clearly identifies planned operations outside paths.

#### Scenario: Contract matches current implementation
- GIVEN the current in-memory server
- WHEN a consumer inspects the OpenAPI paths
- THEN POST /v1/loans and GET /v1/loans/{id}/payoff are the only executable operations
- AND their documented response status sets match the handler behavior
- AND lifecycle operations are explicitly marked planned, not callable.
