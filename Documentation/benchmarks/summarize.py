"""Write medians and min/max variation from Go benchmark logs as CSV.

Reads the raw logs written by run.sh and writes summary.csv beside them. Both
are generated output: the results directory is not tracked.

Usage: python3 Documentation/benchmarks/summarize.py
       python3 Documentation/benchmarks/summarize.py /path/to/results
"""

import csv
from pathlib import Path
import statistics
import sys


def main():
    directory = Path(sys.argv[1]) if len(sys.argv) > 1 else Path(__file__).parent / "results"
    rows = []
    for path in sorted(directory.glob("sync-*.txt")):
        groups = {}
        for line in path.read_text().splitlines():
            if not line.startswith("BenchmarkSync"):
                continue
            parts = line.split()
            metrics = {
                parts[i + 1]: float(parts[i])
                for i in range(2, len(parts) - 1, 2)
            }
            groups.setdefault(parts[0], []).append(metrics)
        for name, samples in groups.items():
            for metric in sorted(samples[0]):
                values = [sample[metric] for sample in samples]
                rows.append([
                    path.name, name, metric, len(values),
                    statistics.median(values), min(values), max(values),
                ])
    with (directory / "summary.csv").open("w", newline="") as output:
        writer = csv.writer(output)
        writer.writerow(["source", "benchmark", "metric", "runs", "median", "min", "max"])
        writer.writerows(rows)


if __name__ == "__main__":
    main()
