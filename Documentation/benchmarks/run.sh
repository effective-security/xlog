#!/usr/bin/env bash
# Run from any directory. Set TMPDIR to a disk-backed directory for file results.
set -euo pipefail
cd "$(dirname "$0")/../.."

out=${XLOG_BENCH_OUT:-Documentation/benchmarks/results}
mkdir -p "$out"
work=$(mktemp -d)
trap 'rm -r "$work"' EXIT

{
  date -u
  git rev-parse HEAD
  git status --short
  go version
  go env GOOS GOARCH GOAMD64
  uname -a
  lscpu
  df -T . "${TMPDIR:-/tmp}"
  printf 'GOMAXPROCS=8; count=3; throughput benchtime=200ms\n'
} > "$out/environment.txt"

go test -run '^$' -bench '^BenchmarkSyncPipeline$' -benchmem -benchtime=200ms -count=3 -cpu=8 . > "$out/sync-throughput.txt"
go test -run '^$' -bench '^BenchmarkSyncAPI$' -benchmem -benchtime=200ms -count=3 -cpu=1,8 . > "$out/sync-api.txt"
go test -run '^$' -bench '^BenchmarkSyncSinks$' -benchmem -benchtime=200ms -count=3 -cpu=8 . > "$out/sync-sinks.txt"
go test -run '^$' -bench '^BenchmarkSyncLatency$/././(discard|file)$' -benchmem -benchtime=10000x -count=3 -cpu=8 . > "$out/sync-latency.txt"
go test -run '^$' -bench '^BenchmarkSyncLatency$/././slow$/./bytes=64$' -benchmem -benchtime=1000x -count=3 -cpu=8 . > "$out/sync-slow-latency.txt"
GOMAXPROCS=8 XLOG_PERF_LOAD=1 go test -run '^TestLogging(Load|Retained)$' -count=3 -v . > "$out/load.txt"

# Profile separately: profiling overhead must not enter baseline numbers.
go test -run '^$' -bench '^BenchmarkSyncPipeline$/text/kv/discard/fields=4$/bytes=64$/caller=true$/parallel=true$' \
  -benchtime=3s -cpu=8 -mutexprofile="$work/mutex.out" -blockprofile="$work/block.out" \
  -cpuprofile="$work/cpu.out" -o "$work/xlog.test" . > "$out/profile-run.txt"
for profile in cpu mutex block; do
  go tool pprof -top "$work/xlog.test" "$work/$profile.out" > "$out/profile-$profile.txt"
done
go tool pprof -list=internalLog "$work/xlog.test" "$work/mutex.out" > "$out/profile-output-lock.txt"
