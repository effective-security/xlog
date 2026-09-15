# RULES OF CONDUCT

This module contains the xlog logging library, logrotate, and stackdriver. There is no
service, agent loop, `cmd/`, or generated mock tree. Work in one package
at a time unless you are changing a documented internal dependency.

## NAVIGATION

Do not start by grepping the tree.

1. Open [`Documentation/codemap.md`](Documentation/codemap.md). Use the
   concept index to find the owning file, then the package section for
   entry points and invariants.
2. Open that file (and its `_test.go`) before searching elsewhere.
3. Grep or glob only if the concept is missing from the map. When you find
   it, add it to the map in the same change.
4. For high-level purpose and install path, see [`README.md`](README.md).

## CODING GUIDELINES

### Style

- Do not use long one-liners for map or struct population; split
  key-value pairs on new lines for readability.
- Do not use many hardcoded strings or integers; define `const` at the
  top of the file or in the package.
- Memoize into variables; do not call functions with the same parameters
  more than once.

  Bad example:

```go
  if len(r.Tools) > 0 {
	  ts := make([]any, 0, len(r.Tools))
  }
```

### Errors

- Use `github.com/cockroachdb/errors` for all error creation and wrapping.
- Wrap external errors (filesystem, YAML/JSON, network, reflection, etc.)
  using either:
  - `errors.WithMessage(err, "unable to read file")` for static context.
  - `errors.Wrapf(err, "failed to resolve file %s", path)` when context
    includes dynamic values.
- Errors originated from internal sentinel types must also be wrapped so
  the stack is preserved. For `var ErrNotFound = errors.New("not found")`
  do not simply `return ErrNotFound`.
- Never ignore errors from serialization, filesystem, or network calls in
  library paths that already return `error`. Helpers that intentionally
  coerce and swallow errors (for example `xlog.EscapedString`, `stackdriver.String`)
  must keep that behavior documented in `Documentation/codemap.md`.
- Preserve sentinel errors used by callers (`errors.Is` / `errors.As`).
- Wrap with context that identifies the failed operation without losing
  the original cause.
- Keep error strings accurate after refactors. Do not leave stale package
  or function names in runtime errors.
- Panic is part of some public APIs (`MustRepoLogger`, `ContextWithKV` on
  malformed entries, `LogLevel.Char` / `String` on invalid levels, and logger
  panic methods). Do not convert those to returned
  errors unless the package contract is being changed on purpose — and
  then update the codemap.

### Tests

- Build tests using `assert` and `require` from
  `github.com/stretchr/testify`.
- Assert exact behavior, including key absence versus empty values and
  the real `err` from the call being tested.
- Keep test setup and cleanup trustworthy: fixtures should match their
  comments, and tests should actually execute in CI.
- Prefer table tests for conversion and formatting helpers.
- Put extra coverage in `*_extra_test.go` when extending an already large
  test file. Use `package foo_test` for black-box tests.
- Override and restore `xlog.TimeNowFn` and `xlog.ExitFunc` in serial tests.
  Prefer `testing/synctest` or channel coordination for worker tests instead of
  wall-clock polling. Do not run global formatter/configuration mutations in parallel.
- This module has no gomock-generated interfaces; do not introduce mocks
  unless the package under test cannot be exercised directly.

### Tools

- `make generate` : generate artifacts if interfaces require it (most
  packages here have none)
- `make fmt` : apply go fmt
- `make test` : test entire project
- `make lint` : final check
- `make all` : clean, tools, generate, coverage test

CI sets `MIN_TESTCOV=80` in `.github/workflows/unittest.yml`; the current status
comparison is strictly greater than 80%. Coverage/race limitations are in FINDINGS.md.

### Documentation

- Every package has a `doc.go` with a package comment and, where the
  package has a non-obvious entry point, a short usage example.
- Document all exported types, functions, interfaces, and interface
  methods. Say what a symbol is _for_, not just what it is named.
- Keep samples in `doc.go`, package `README.md`, and `Documentation/`
  accurate against the current API. A wrong sample is worse than no sample.
- When you add a package, add it to the root `README.md` table, add
  `doc.go`, and add a section plus concept-index rows in
  `Documentation/codemap.md`.

#### Keep `Documentation/codemap.md` current

Update the codemap in the **same change** when you add functionality or
make a major change, including:

- New, removed, or renamed package, subpackage, or file that owns a
  concept.
- New, moved, or renamed exported entry-point type, func, or interface.
- Changed invariants: panic vs error, process-global state, field ownership,
  formatter output, filtering, buffering, goroutine/callback
  rules, or internal imports between packages.
- Changed test layout or testdata conventions.

The map must stay the navigation index: concept → file → entry points →
invariants. If you had to grep to find something that belongs there, add
the row.

## REPOSITORY MAP

Start here instead of grepping the tree.

- **[`Documentation/codemap.md`](Documentation/codemap.md)** — concept
  index, per-package files and entry points, invariants, internal
  dependencies, test layout.
- **[`README.md`](README.md)** — high-level overview and quick-start
  samples.
- Package `doc.go`: root, `logrotate/doc.go`, and `stackdriver/doc.go`.
- **[`FINDINGS.md`](FINDINGS.md)** — bugs, evidence, severity, and validation limits.
- **[`ROADMAP.md`](ROADMAP.md)** — future improvements and optional buffered ingress.
