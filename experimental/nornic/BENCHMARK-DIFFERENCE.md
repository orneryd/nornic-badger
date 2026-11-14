# Batch decoder benchmark difference report

Windows/amd64, Intel i9-9900KF, Go 1.25.4. Each cell decodes 4,096 values into the same preallocated output buffer. Three repetitions, CPU=1, 300 ms per cell. Values are medians; these are local microbenchmarks, not physical I/O or NornicDB query results.

| Shape | Scalar ns/block | Batch ns/block | Raw ns/block | Batch vs scalar | Batch vs raw | Encoded bytes |
|---|---:|---:|---:|---:|---:|---:|
| constant | 13762 | 1096 | 4182 | 12.56x faster | 0.26x raw time | 20 |
| small-range | 34465 | 10738 | 4284 | 3.21x faster | 2.51x raw time | 3092 |
| monotone | 39342 | 10679 | 4194 | 3.68x faster | 2.55x raw time | 6164 |
| high-entropy | 15465 | 4120 | 4155 | 3.75x faster | 0.99x raw time | 32788 |

Raw storage with the same header is 32,788 bytes. All decoder variants allocate zero bytes per benchmark operation; output-buffer construction is excluded equally.

The batch kernel dispatches once per column, fills constants directly, and extracts packed values with word loads including cross-word and tail handling. Raw mode uses direct little-endian decoding. Compression reduces input size but does not universally improve CPU time. The next experiments should evaluate selection/predicate kernels and block-backed cold reads; this result does not justify switching NornicDB storage. No SIMD or engine physical-read modification is implemented.

Validation: package tests and race tests pass; experimental-package vet passes. All bit widths, signed extrema, empty columns, short tails and output-length rejection are tested. Existing integration tests exercise encrypted/plaintext inline/value-log storage, pending writes, snapshot isolation and reopen. Full upstream qualification and NornicDB integration remain unrun.

Receipts: [benchmarks](results/batch/bench.txt), [tests](results/batch/tests.txt), [race](results/batch/race.txt). Original baseline remains in results/baseline.
