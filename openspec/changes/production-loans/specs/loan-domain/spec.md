## ADDED Requirements

### Requirement: Underwrite and generate exact schedules
The domain SHALL validate affordability, explicit currency and scale, and generate principal-conserving annuity or straight-line schedules using named rounding and day-count conventions.

#### Scenario: Zero-rate schedule
- GIVEN a valid zero-rate term loan
- WHEN a schedule is generated
- THEN each instalment is rounded to minor units and the final instalment absorbs any remaining principal
- AND the sum of principal instalments equals the original principal exactly.

#### Scenario: Affordability
- GIVEN positive monthly income and nonnegative debt and payment
- WHEN debt plus payment exceeds the maximum debt ratio
- THEN underwriting rejects the application.
