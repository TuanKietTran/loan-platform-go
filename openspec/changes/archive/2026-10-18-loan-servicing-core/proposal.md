# Agreement servicing core checkpoint

Implement a pure agreement aggregate before adding I/O orchestration. Preserve legacy create/payoff routes until lifecycle handlers and downstream adapters are ready. No runtime, persistence, fraud HTTP or payment HTTP integration claim is made by this checkpoint.

The aggregate validates synthetic applications, applies affordability and supplied fraud decisions, requires settled payment evidence for cash movements, accrues dated ACT/365F interest, assesses fixed late fees once per overdue instalment, allocates repayments and regenerates partial-prepayment schedules. Payment evidence authenticity and replay persistence remain responsibilities of later adapters/store commands.
