# Definition of done

A box is **Met** only when the code behaves that way and a test asserts it. **Partial** means the code does it, but no test asserts that scenario. **Unmet** means it is not implemented.

Playback storyboard: `openspec/gifs/playback-flow.gif`.

## Met

| Done when | Evidence |
| --- | --- |
| A keyed create returns `loan_id` and a payoff of principal 10 with no accrued interest | Playback. |
| Replaying that key is HTTP 400 with the original id and the payoff stays 10 | Playback. |
| A create with no key originates a second loan | `TestLoanWithoutKeyCreatesAnother`. |
| 1000 creates finish in under 60 seconds, with one HTTP 503 retried on each call | Playback test, 80ms. |
| HTTP 400 is not retried | Playback unit test. |
| Statement coverage is measured | 61.6%. |

## Partial

| Done when | Why it is not Met |
| --- | --- |
| Unknown loan is HTTP 404 | Implemented. No playback assertion. No dedicated test calls a missing id. |
| Invalid id is HTTP 400 | Implemented. No test sends one. |
| Invalid JSON is HTTP 400 | Implemented. No test sends a bad body. |
| Zero or negative principal is accepted | Implemented. No test posts zero. |

## Unmet

| Not done | Why |
| --- | --- |
| Record repayment, accrue on the API, get loan, get schedule | Named in the README. No HTTP route. |
| Interest actually changes a quote | `AccrueInterest` exists on the repository and the server never calls it. A new loan's payoff is always the principal. |
| Postgres, including `borrower_id` | The migration is unused. Create has no borrower. |
| Coverage at a high bar | 61.6%. Accrual and `cmd/server` are uncovered. No 80% gate. |

## GIF review

Good enough. Retry stays at 0 loans. First success shows payoff $10. Playback is HTTP 400 and the payoff is still $10. Storyboard, not a screen capture.
