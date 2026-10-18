# Atomic agreement command store

Add an agreement repository port and process-local memory implementation with atomic keyed creation and pure mutation transactions. A replay returns its original response snapshot, not the latest agreement; changed payloads conflict. Commands orchestrate validation and replayable fraud screening outside store locks, using a stable namespaced event identity and injectable agreement IDs. Disbursement and repayment accept evidence only from future trusted adapters, not caller-supplied HTTP input.

This checkpoint does not implement HTTP routes, downstream clients, a durable outbox or SQL persistence. Those remain mandatory in production-loans.
