# Release checklist

1. Start from a clean working tree and review `CHANGELOG.md`.
2. Run `gofmt`, `go mod tidy`, `go vet ./...`, `staticcheck ./...`, and
   `golangci-lint run ./...`.
3. Run `go test ./...` and `go test -race ./...`.
4. Run `govulncheck ./...` with the version pinned in CI.
5. Run the hostile-byte array and codec fuzz targets for at least two minutes
   each, then run the longer scheduled fuzz workflow.
6. Confirm native Linux amd64 and arm64 CI jobs pass.
7. For pre-`v1.0.0` releases, review intentional wire changes and update
   `FORMAT.md` and the golden framing tests together. The `v1.0.0` tag freezes
   that exact version 1 layout; after it, incompatible changes require a new
   format version and a reader that retains version 1 support.
8. Run the selector benchmark smoke job and compare important workloads with
   the previous release when changing hot paths.
9. Tag only the exact reviewed commit. Do not tag a dirty working tree.
