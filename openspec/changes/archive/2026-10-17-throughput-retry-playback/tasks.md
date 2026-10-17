# Tasks

## 1. Playback client

- [x] Send a recorded call list in order
- [x] Retry transport errors and HTTP 408, 429, and 5xx up to the attempt limit
- [x] Do not retry HTTP 400
- [x] Treat `idempotency key already used` as a replay, not a retry

## 2. Prove the rate

- [x] 1000 calls, one forced HTTP 503, then success
- [x] Finish in under 60 seconds
- [x] Play the same list again and check stored totals

## 3. Coverage

- [x] `go test -count=1 -coverpkg=./... ./...`
