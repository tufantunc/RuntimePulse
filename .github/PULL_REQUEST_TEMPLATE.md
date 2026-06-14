## Summary

<!-- What does this change and why? -->

## How verified

<!-- Commands you ran and their result. -->

- [ ] `go test -race ./...`
- [ ] `go vet ./...` and `gofmt -l .` (prints nothing)
- [ ] `./scripts/smoke.sh` ends `SMOKE OK`
- [ ] `GOOS=windows go build ./... && GOOS=windows go vet ./...`

## Notes

<!-- Anything reviewers should know. Link related issues/discussions. -->
