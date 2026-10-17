# 1000 requests, retry, and playback

## Why

Each service needs to absorb 1000 calls inside a minute, retry a transient failure, and play the same calls again without corrupting state.

## What

- `internal/playback` client with a retry policy.
- A test that plays 1000 calls through the real HTTP routes, retries one HTTP 503 each, then plays the list again.
- Coverage recorded from `go test -coverpkg=./...`.

## Impact

- Spec `playback` added.
- Portfolio and loan accept an optional `idempotency_key` so replay is safe. Omitting the key keeps the previous apply-every-time behavior.
