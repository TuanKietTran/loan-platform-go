# Tasks

- [x] Move create/replay arbitration and canonical identity into repository lock.
- [x] Prevent existing ID overwrite and key reservation on failed inserts.
- [x] Reject invalid terms and blank/oversized keys in the command boundary.
- [x] Propagate cancellation and use typed conflict mapping for HTTP 409.
- [x] Preserve existing tests and add concurrent-handler, replay-state, adapter, query and handler regressions.
- [x] Document deliberate invalid-term and replay contract corrections.
- [x] Correct current OpenAPI payoff fields to PascalCase decimal strings and document header/body key alternatives; assert serialized response field names.
