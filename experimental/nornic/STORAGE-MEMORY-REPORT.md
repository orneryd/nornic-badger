# Storage size and query allocation experiment

This extends the [batch decoder report](BENCHMARK-DIFFERENCE.md). Both storage and query memory matter. Encoded-size improvements alone do not prove either total database savings or lower process RSS.

## Measured Badger file footprint

Each cell stores 1,048,576 exact int64 values in 256 blocks, then cleanly closes Badger and sums every regular file in its directory. It reopens and verifies every value. Results are median file bytes from three runs, using identical keys, sync-writes enabled and upstream default Snappy compression. Thresholds exercise inline and value-log placement. The raw reference is packed little-endian int64, not JSON or object maps. Column values vary across blocks; these are synthetic shapes, not production graph data.

| Shape | Placement threshold | Raw directory bytes | Column directory bytes | Directory reduction |
|---|---|---:|---:|---:|
| constant | 1 B (value log) | 9457572 | 1073243 | 88.65% |
| constant | 1 MiB (inline) | 1491583 | 1053611 | 29.36% |
| small-range | 1 B (value log) | 9457572 | 1860086 | 80.33% |
| small-range | 1 MiB (inline) | 5266304 | 1127802 | 78.58% |
| monotone | 1 B (value log) | 9457572 | 2646556 | 72.02% |
| monotone | 1 MiB (inline) | 5274171 | 2650756 | 49.74% |
| high-entropy | 1 B (value log) | 9457572 | 9462710 | -0.05% |
| high-entropy | 1 MiB (inline) | 9461628 | 9466758 | -0.05% |

Total files include Badger metadata, table/index overhead and value-log files. Roughly 1 MiB of fixed engine overhead remains in even the smallest fixture. File lengths are not allocated filesystem sectors. These are clean-close snapshots, not post-churn/forced-compaction steady state. Database contents are otherwise identical; no indexes, constraints or correctness checks were disabled selectively. No encryption is used in this footprint comparison; encrypted compatibility remains covered by the separate integration tests.

High-entropy columns grow slightly because they choose raw encoding and add the column header. Upstream SSTable compression already reduces some raw fixtures substantially; quoting only codec ratios would exaggerate the incremental storage benefit.

## Query allocation and memory

CountEqual counts exact matches on the borrowed encoded column without expanding an int64 array. All modes answer the same equality-count query on 4,096 values. Allocate/decode/count allocates its output per query; reuse/decode/count keeps a 32 KiB scratch array. Encoded bytes and reusable buffers are built outside timing for every mode. Medians of three CPU=1, 300 ms repetitions on Windows/amd64, i9-9900KF, Go 1.25.4:

| Shape | Encoded count ns | Allocate/decode/count ns | Reuse/decode/count ns | Encoded query allocation | Allocating reference |
|---|---:|---:|---:|---|---|
| constant | 3.239 | 6161 | 3021 | 0 B, 0 allocations | 32,768 B, 1 allocation |
| small-range | 25002 | 16522 | 12732 | 0 B, 0 allocations | 32,768 B, 1 allocation |
| monotone | 32734 | 15938 | 12623 | 0 B, 0 allocations | 32,768 B, 1 allocation |
| high-entropy | 4390 | 9153 | 6185 | 0 B, 0 allocations | 32,768 B, 1 allocation |

Reuse/decode/count also has zero per-query allocation, but retains its 32 KiB scratch array. The encoded operation needs neither that array nor a selection vector for this count-only result. It retains the already encoded input. Constant columns answer from their representation; packed columns currently use the scalar packed extraction owner, so memory reduction does not guarantee faster CPU time. No claim is made for arbitrary predicates, full result materialization or whole-process memory reduction.

## Decision and next work

Keep storage-size and allocation optimization in the experiment. Compression shows incremental file savings for compressible fixtures, but data shape and placement matter. Avoid decoding a column when the requested operator can consume it directly. Compare against reused-buffer execution as well as allocation-heavy execution. Next candidates are batch packed predicates, compact property directories for small records, bounded block reads and retained-memory/RSS tests on isolated processes. No new codec or physical Badger read path is introduced in this step.

The process peak RSS, page-cache residency, encrypted footprint, write amplification, compaction churn and end-to-end NornicDB workloads remain unmeasured. This is not a basis for production storage promotion.

## Validation and receipts

Package tests, race tests and experimental-package vet pass. The opt-in footprint test passes all 16 cases in each of three repetitions, verifying all reopened values. Predicate tests cover empty/constant data, signed extremes, absent targets and every packed width. The opt-in test is skipped by the default package command and was run separately; it was not included in the race run.

Raw receipts: [footprint](results/storage-memory/footprint.txt), [allocation benchmarks](results/storage-memory/bench.txt), [tests](results/storage-memory/tests.txt), [race](results/storage-memory/race.txt), [vet](results/storage-memory/vet.txt). Inherited root vet findings remain recorded in the initial baseline.

Reproduce the footprint run with NORNIC_FOOTPRINT_EXPERIMENT=1 and `go test ./experimental/nornic -run ^TestBadgerStorageFootprint$ -count=3 -v`. Reproduce allocations with `go test ./experimental/nornic -run ^$ -bench ^BenchmarkPredicateMemory$ -benchmem -benchtime=300ms -count=3 -cpu=1`. Run timing experiments separately from storage tests.
