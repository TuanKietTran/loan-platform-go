# Loan design

- `LoanRepository` is `Save` and `GetByID`. Create always sets status `PENDING`, outstanding principal to the posted principal, and accrued interest to 0.
- Payoff total is `OutstandingPrincipal + AccruedInterest`. Nothing in the HTTP path changes accrued interest after create, so a quote on a newly created loan is the principal.
- `AccrueInterest(id, days)` adds `outstanding * (interest rate / 365) * days` and is not mounted. A missing loan returns `loan not found`.
- `migrations/001_init.sql` defines `loans` and `repayments`, including a required `borrower_id`. The server does not open Postgres, and the create command has no borrower id.
