# Findings

Open defects and validation gaps only. Review date: 2026-09-19; scope is all
three library packages, their tests, build recipes, and CI configuration.
Severity reflects impact when triggered, not an assigned vulnerability score.
Items are removed once closed, so resolved findings live in git history.
Features and implementation milestones belong in [ROADMAP.md](ROADMAP.md).

## Summary

| ID   | Severity | Status                         | Issue                                                                                |
| ---- | -------- | ------------------------------ | ------------------------------------------------------------------------------------ |
| F-06 | Medium   | Open, reproduced               | Invalid configuration silently disables noncritical logging; defaults do not persist |
| F-07 | High     | Open, source review            | Recursive logging can deadlock; formatters without a sink serialize every call       |
| F-09 | Medium   | Open, source review            | Length options do not bound fields or whole records; truncation can split UTF-8      |
| F-10 | Medium   | Open, reproduced/source review | CI/race/coverage checks and test isolation have gaps                                 |

## F-06 — Configuration errors and defaults can hide logs

Owner: [logmap.go](logmap.go), `SetRepoLevel`, `NewPackageLogger`, global/repo
setters; [init.go](init.go).

SetRepoLevel discards ParseLevel's error and applies its CRITICAL fallback.
An invalid value was confirmed to make `p.LevelAt(xlog.ERROR)` false. All other
ordinary levels become hidden too. NewPackageLogger always assigns INFO, so
global/repository changes (including XLOG_LEVEL initialization) do not become
defaults for later registrations. Unknown targets are silently ignored.

Validate configuration before applying it. A future checked API should return
errors and reject a whole invalid configuration without partial mutation, then
define inheritance for new registrations and derived loggers explicitly.

## F-07 — Reentrancy can deadlock; formatters without a sink serialize

Owner: [packagelogger.go](packagelogger.go), `internalLog`/`internalLogf`;
[logmap.go](logmap.go), `acquireOutput` and the output lock.

A formatter that writes to its destination inline, which is every formatter
without a `Sink`, takes the output lock exclusively, so one slow destination
still stalls every other package for the duration of each write.

Recursive logging can still deadlock. A formatter, destination writer, Stringer,
or MarshalJSON implementation that emits through the same output path re-enters
the output lock, or re-enters a destination write that is already in progress.
Unguarded recursive ERROR observers still recurse indefinitely. A blocked
destination also blocks producers once a sink's queue fills, by design.

This is established by source structure, not by a measured regression. Fix
direction: define an explicit reentrancy policy, either a detectable recursion
guard or a documented contract with a test that proves the failure mode, and
decide whether inline formatters should also render off the lock.

## F-09 — Value and record bounds are incomplete

Owners: [options.go](options.go), [formatters.go](formatters.go),
[json_formatter.go](json_formatter.go), [stackdriver/sd.go](stackdriver/sd.go),
[logrotate/channelwriter.go](logrotate/channelwriter.go).

`MaxLogLength` limits only rendered KV values in text and only the plain `msg`
in JSON. Stackdriver ignores it and uses the separate mutable global
`MaxLogMessageLength`. Nothing bounds a whole record, and truncation happens
after the full value is allocated and rendered. Byte slicing can split a UTF-8
sequence or a text escape sequence. Stackdriver always computes caller metadata
even when caller output is disabled.

ChannelWriter bounds queued item count, not bytes. It allocates and copies
before blocking on admission, so pending producers add memory outside the queue,
and its buffer pool has no retained-capacity cap, unlike the text encoder and
record buffer pools.

Define per-field and whole-record limits with Unicode-safe truncation, fold the
Stackdriver global into `Config`, and give ChannelWriter a byte budget and a
pool cap, or migrate it to the record sink.

## F-10 — Validation infrastructure has blind spots

Owners: [.project/gomod-project.mk](.project/gomod-project.mk),
[.github/workflows/unittest.yml](.github/workflows/unittest.yml), existing tests.

- `make covtest RACE=true` selects `-race` with `-covermode=count`; Go requires
  atomic coverage mode for race instrumentation. A direct check reported
  `-covermode must be "atomic", not "count", when -race is enabled`.
  CI does not set RACE=true.
- `go_test_cover`, the recipe `covtest` uses, pipes go test output through grep
  without pipefail and infers failure from the last output line. It does not
  reliably preserve the command's exit status. Use direct exit-status handling
  or Go test JSON output.
- CI compares coverage with strict `> ${MIN_TESTCOV}` (90) rather than `>=`, and
  the reporting step only posts a commit status through curl; it never fails the
  job. Whether that status blocks merging depends on repository branch
  protection, which was not inspected.
- Older tests do not consistently restore globals, and the original channel
  tests still poll real time. New concurrency regressions use testing/synctest
  or channel coordination; converting the rest would remove the remaining
  wall-clock dependence.
- Some assertions do not test their stated behavior: `Test_StringFormatter`
  calls `assert.Contains` with the literal as the haystack and an empty actual
  result as the needle for a disabled log, which passes trivially. Prefer exact
  empty-output assertions.

These gaps explain why a passing race/coverage run does not clear the open
issues. Repair the gates and add focused regression tests as each finding closes.
