# Issues

(none yet)

## [2026-04-27] Task 17a: controllers legacy removal
- `GOOS=linux go build ./...` still fails in `pkg/server/server.go` because it references removed controller Config/ChangeTracker types
- Package-level controller build and controller tests pass
