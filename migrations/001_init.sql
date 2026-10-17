CREATE TABLE loans (
    id UUID PRIMARY KEY,
    borrower_id TEXT NOT NULL,
    principal NUMERIC(20,6) NOT NULL,
    interest_rate NUMERIC(10,6) NOT NULL,
    term_months INT NOT NULL,
    status TEXT NOT NULL,
    outstanding_principal NUMERIC(20,6) NOT NULL,
    accrued_interest NUMERIC(20,6) NOT NULL DEFAULT 0,
    disbursed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE repayments (
    id UUID PRIMARY KEY,
    loan_id UUID NOT NULL REFERENCES loans(id),
    amount NUMERIC(20,6) NOT NULL,
    principal_portion NUMERIC(20,6) NOT NULL,
    interest_portion NUMERIC(20,6) NOT NULL,
    paid_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
