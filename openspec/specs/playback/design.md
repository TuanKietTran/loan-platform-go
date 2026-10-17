# Playback design

- Package `internal/playback`. `Play` sends calls in order. `Policy.MaxAttempts` defaults to 3. `Policy.BaseDelay` defaults to 0 and doubles each extra attempt when set.
- Retryable statuses are 408, 429, and any status at or above 500. Context cancellation is not retried.
- `IsIdempotentReplay` matches HTTP 400 whose body contains `idempotency key already used`.
- `TestThousandPerMinuteAndPlayback` puts a one-shot HTTP 503 in front of the real Chi routes, plays 1000 calls, then plays them again.
