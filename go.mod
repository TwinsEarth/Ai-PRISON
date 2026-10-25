module github.com/ai-prison/ai-prison

// Minimum Go version: code review of every import and language construct
// found nothing newer than the min/max builtins (Go 1.21) — no slices/maps/cmp
// (1.21+, still fine), no errors.Join, no enhanced http.ServeMux patterns, no
// range-over-func. 1.23 is the floor reserved for the CI matrix
// (1.23 / 1.24 / 1.25); a newer toolchain builds it unchanged.
go 1.23

// AI Prison is a pure-Go, standard-library-only prototype.
// There are intentionally no third-party dependencies in the first
// release so the project builds offline and cannot be broken by
// upstream module availability.
