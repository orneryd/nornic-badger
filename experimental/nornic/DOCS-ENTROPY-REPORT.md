# Documentation-seeded mixed-entropy experiment

## Corpus and method

Input: NornicDB documentation at commit `df07c94fb7df5bb05a0c854cdaa0b8b6eae8182a` (clean docs directory). The manifest records every Markdown path and SHA-256. Corpus digest: `b24c26eeb64f48ca159d33a7a3fa18978fe849f0eb9c0623fe19aaea28176f9f`.

318 documents: the first 16 sorted paths supply up to 4 KiB each for a shared raw Zstd dictionary and are excluded from all evaluation variants. The remaining 302 documents seed 38,656 generated records. Each record has a stable path-derived identity, document path, status, body excerpt, fixed-epoch timestamp and independent SHA-256-derived nonce. Original documentation is also tested in blocks up to 32 KiB. Numeric columns use 128-value blocks; the separate outlier sweep uses 4,096 values. This is synthetic documentation-derived data, not a production graph snapshot.

Each shape/codec is stored in its own actual Badger database with identical stable keys, default Snappy/table settings and synchronous writes. Close, sum every regular file, reopen, fetch every key and verify every decoded byte. Dictionary variants also store the shared dictionary in the database and include its uncompressed size in encoded payload totals. File lengths are not filesystem allocated sectors. There is no additional graph index, entity mapping, nullable/type bitmap or mutation overlay in this experiment.

Results are medians of three local Windows/amd64 runs. Codec CPU timings cover bulk encoding and full materializing decode/verification, not database request latency. Allocations come from separate Go benchmarks and exclude retained codec/dictionary state. Default Badger caches and approximately 1 MiB fixed engine metadata remain present. These are clean-close, warm-process comparisons, not cold-device or compaction/churn results. No whole-process RSS or physical partial-read claim is made.

## Total database footprint

Raw means packed bytes stored through normal Badger, including its existing Snappy compression. Best is the smallest measured total directory for this shape, not a proposed universal default.

| Shape | Raw Badger bytes | Smallest candidate | Candidate Badger bytes | Reduction |
|---|---:|---|---:|---:|
| decimal-float-bits | 1,246,164 | anchored-delta | 1,070,016 | 14.14% |
| documentation | 3,263,408 | dictionary-zstd | 2,534,077 | 22.35% |
| independent-hashes | 2,340,951 | raw | 2,340,951 | 0.00% |
| low-range | 1,211,276 | for | 1,084,159 | 10.49% |
| mixed-json | 4,682,722 | dictionary-zstd | 2,747,109 | 41.34% |
| outliers | 1,220,304 | patched-for | 1,095,337 | 10.24% |
| random-int64 | 1,388,252 | raw | 1,388,252 | 0.00% |
| timestamps | 1,162,703 | anchored-delta | 1,063,523 | 8.53% |
| unique-paths | 1,384,288 | dictionary-zstd | 1,134,069 | 18.08% |

## Candidate receipts

Encoded bytes include candidate framing and dictionary bytes, before normal Badger compression. Times are bulk milliseconds for the entire shape; decode includes correctness comparison. Decode B/block is the average allocated bytes per complete materialized block. SIMD, FSST and ALP are not implemented here.

| Shape | Codec | Encoded bytes | Encode ms | Decode/check ms | Decode B/block |
|---|---|---:|---:|---:|---:|
| decimal-float-bits | anchored-delta | 23,630 | 0.744 | 1.025 | 6,144 |
| decimal-float-bits | for | 233,272 | 1.184 | 0.000 | 2,048 |
| decimal-float-bits | patched-direct | 310,726 | 1.486 | 1.075 | 1,024 |
| decimal-float-bits | raw | 309,248 | 0.000 | 0.000 | 1,024 |
| decimal-float-bits | sampled-zstd | 103,910 | 5.583 | 1.481 | 1,153 |
| decimal-float-bits | shuffle-zstd | 90,671 | 2.964 | 0.566 | 2,178 |
| decimal-float-bits | xor-zstd | 60,257 | 2.368 | 0.513 | 2,177 |
| decimal-float-bits | zstd | 103,608 | 3.534 | 1.635 | 1,153 |
| documentation | adaptive-zstd | 1,559,444 | 24.975 | 11.527 | 13,956 |
| documentation | dictionary-zstd | 1,481,680 | 32.447 | 15.503 | 13,951 |
| documentation | raw | 4,262,325 | 1.505 | 1.006 | 12,998 |
| documentation | sampled-zstd | 1,714,882 | 25.953 | 10.302 | 13,911 |
| documentation | snappy | 2,159,302 | 8.242 | 3.603 | 12,997 |
| documentation | zstd | 1,559,103 | 24.326 | 10.817 | 13,952 |
| independent-hashes | adaptive-zstd | 1,237,294 | 1.259 | 1.010 | 4,096 |
| independent-hashes | dictionary-zstd | 1,293,278 | 4.853 | 0.000 | 4,864 |
| independent-hashes | raw | 1,236,992 | 0.615 | 0.503 | 4,096 |
| independent-hashes | sampled-zstd | 1,237,294 | 1.170 | 0.000 | 4,096 |
| independent-hashes | snappy | 1,238,502 | 0.510 | 0.564 | 4,096 |
| independent-hashes | zstd | 1,241,220 | 1.732 | 0.528 | 4,864 |
| low-range | adaptive-zstd | 36,500 | 2.122 | 1.053 | 1,152 |
| low-range | anchored-delta | 33,824 | 0.528 | 0.557 | 6,144 |
| low-range | delta-mini-for | 31,408 | 0.411 | 0.545 | 3,072 |
| low-range | dictionary-zstd | 87,690 | 3.221 | 1.153 | 1,152 |
| low-range | for | 25,368 | 0.535 | 0.525 | 2,048 |
| low-range | mini-for | 26,576 | 0.000 | 0.781 | 3,072 |
| low-range | patched-direct | 25,670 | 0.578 | 0.570 | 1,024 |
| low-range | patched-for | 25,670 | 1.128 | 0.000 | 2,048 |
| low-range | raw | 309,248 | 0.747 | 0.000 | 1,024 |
| low-range | sampled-zstd | 36,500 | 3.949 | 1.028 | 1,152 |
| low-range | shuffle-zstd | 30,039 | 0.708 | 1.100 | 2,177 |
| low-range | snappy | 149,617 | 0.447 | 0.504 | 1,024 |
| low-range | xor-zstd | 36,057 | 2.662 | 1.327 | 2,177 |
| low-range | zstd | 36,198 | 2.165 | 1.108 | 1,152 |
| mixed-json | adaptive-zstd | 1,666,165 | 25.364 | 10.933 | 57,155 |
| mixed-json | dictionary-zstd | 1,699,738 | 33.700 | 11.950 | 57,146 |
| mixed-json | raw | 15,540,713 | 2.681 | 4.714 | 57,126 |
| mixed-json | sampled-zstd | 1,666,165 | 28.320 | 11.610 | 57,161 |
| mixed-json | snappy | 3,598,183 | 6.435 | 5.112 | 57,126 |
| mixed-json | zstd | 1,665,863 | 24.312 | 11.222 | 57,146 |
| outliers | adaptive-zstd | 51,243 | 3.531 | 2.034 | 1,152 |
| outliers | anchored-delta | 314,017 | 0.612 | 0.587 | 6,144 |
| outliers | delta-mini-for | 314,672 | 0.537 | 0.605 | 3,072 |
| outliers | dictionary-zstd | 101,896 | 4.152 | 1.997 | 1,152 |
| outliers | for | 310,376 | 0.634 | 0.603 | 2,048 |
| outliers | mini-for | 311,584 | 1.062 | 0.000 | 3,072 |
| outliers | patched-direct | 31,710 | 0.869 | 0.503 | 1,024 |
| outliers | patched-for | 31,710 | 0.512 | 0.525 | 2,048 |
| outliers | raw | 309,248 | 0.351 | 0.000 | 1,024 |
| outliers | sampled-zstd | 51,243 | 5.629 | 1.814 | 1,152 |
| outliers | shuffle-zstd | 54,922 | 2.535 | 1.031 | 2,177 |
| outliers | snappy | 139,464 | 0.347 | 0.503 | 1,024 |
| outliers | xor-zstd | 62,222 | 2.671 | 2.054 | 2,177 |
| outliers | zstd | 50,941 | 3.467 | 1.685 | 1,152 |
| random-int64 | adaptive-zstd | 309,550 | 0.508 | 0.000 | 1,024 |
| random-int64 | anchored-delta | 316,496 | 0.682 | 0.000 | 6,144 |
| random-int64 | delta-mini-for | 316,496 | 0.539 | 0.534 | 3,072 |
| random-int64 | dictionary-zstd | 365,836 | 3.855 | 0.000 | 1,152 |
| random-int64 | for | 315,288 | 0.742 | 0.000 | 2,048 |
| random-int64 | mini-for | 316,496 | 0.337 | 0.000 | 3,072 |
| random-int64 | patched-direct | 310,758 | 1.149 | 1.196 | 1,024 |
| random-int64 | patched-for | 310,758 | 1.464 | 1.520 | 2,048 |
| random-int64 | raw | 309,248 | 0.000 | 0.000 | 1,024 |
| random-int64 | sampled-zstd | 309,550 | 0.504 | 0.000 | 1,024 |
| random-int64 | shuffle-zstd | 313,778 | 0.659 | 0.457 | 2,176 |
| random-int64 | snappy | 310,758 | 0.440 | 0.000 | 1,024 |
| random-int64 | xor-zstd | 313,778 | 0.357 | 0.000 | 2,176 |
| random-int64 | zstd | 313,778 | 0.961 | 0.000 | 1,152 |
| timestamps | adaptive-zstd | 113,854 | 4.417 | 0.629 | 1,152 |
| timestamps | anchored-delta | 9,664 | 0.524 | 0.615 | 6,144 |
| timestamps | delta-mini-for | 302,000 | 1.473 | 0.506 | 3,072 |
| timestamps | dictionary-zstd | 165,610 | 7.996 | 1.223 | 1,152 |
| timestamps | for | 136,504 | 1.064 | 0.000 | 2,048 |
| timestamps | mini-for | 137,712 | 1.006 | 0.000 | 3,072 |
| timestamps | patched-direct | 301,094 | 1.508 | 1.563 | 1,024 |
| timestamps | patched-for | 301,094 | 1.262 | 1.407 | 2,048 |
| timestamps | raw | 309,248 | 0.000 | 0.000 | 1,024 |
| timestamps | sampled-zstd | 113,854 | 6.464 | 0.996 | 1,152 |
| timestamps | shuffle-zstd | 106,908 | 3.181 | 1.030 | 2,176 |
| timestamps | snappy | 235,862 | 0.998 | 0.522 | 1,024 |
| timestamps | xor-zstd | 97,546 | 3.280 | 1.949 | 2,177 |
| timestamps | zstd | 113,552 | 4.188 | 1.030 | 1,152 |
| unique-paths | adaptive-zstd | 86,341 | 3.633 | 2.000 | 9,899 |
| unique-paths | dictionary-zstd | 135,819 | 6.195 | 2.591 | 9,898 |
| unique-paths | raw | 2,806,076 | 0.788 | 0.512 | 9,893 |
| unique-paths | sampled-zstd | 86,341 | 4.620 | 2.029 | 9,900 |
| unique-paths | snappy | 285,706 | 1.533 | 1.036 | 9,893 |
| unique-paths | zstd | 86,039 | 3.704 | 1.807 | 9,899 |

## Outlier sweep

This separate test uses a hash of the documentation README to seed independent row values, then spreads wide exceptions across a 4,096-value low-range column. Sizes below are encoded bytes, not total database bytes. Sweep times are diagnostic only and are not included in the throughput tables.

| Wide values / 4,096 | Existing FOR | Mini-block FOR | Patched packing |
|---|---:|---:|---:|
| 0 | 2,068 | 2,816 | 2,565 |
| 4 | 32,276 | 4,672 | 2,605 |
| 41 | 32,788 | 6,656 | 2,975 |
| 205 | 32,788 | 18,176 | 4,615 |
| 819 | 32,788 | 33,536 | 10,755 |
| 4096 | 32,788 | 33,536 | 32,773 |

## Decisions

- Keep raw fallback for independent hashes and full-range random integers. The sampled Zstd candidate compresses at most 512 bytes before deciding whether to attempt the full payload, then applies an exact 1% minimum-gain check including its one-byte tag. It can miss later structure; this is an intentional CPU/ratio tradeoff, not an entropy proof.
- Retain FOR for clean low-range integers. Patched packing helps sparse wide exceptions; mini-blocks only contain the damage when exceptions are sufficiently sparse. Zigzag and exception metadata can make patched packing worse than FOR when there are no exceptions.
- Store the first absolute timestamp separately from packed deltas. The initial delta prototype let this anchor contaminate the first mini-block. Anchoring reduces timestamp encoded bytes from 302,000 to 9,664 for these fixtures.
- Prefer Zstd candidates for documentation and mixed structured records. A held-out-document dictionary improved documentation footprint further, but its incremental mixed-record benefit is small and it regresses random input. Dictionary selection, residency, format identity and lifecycle remain design work.
- Decode patched values directly into the result byte buffer. This preserves identical bytes while removing an intermediate int64 array. Compare patched-for and patched-direct allocations; this does not eliminate the final requested output.
- Float bit reinterpretation is lossless and still eight bytes per float64. The decimal-float fixture tests shared bit structure through FOR, shuffle, XOR and general compression. Random float bit patterns are covered by random-int64 and explicit signed-zero, infinity, subnormal and distinct NaN-payload round trips. No numeric cast or quantization is used.
- FSST and ALP remain candidates for a later implementation; these results do not substitute Zstd dictionaries for FSST or claim an ALP evaluation. No global sort/permutation or shared-blob deduplication has been introduced. Deduplication needs ownership, visibility and GC design; the source corpus has no identical whole-document hash duplicates.

## Float storage and remaining tradeoffs

The separate CPU=1 Go benchmark writes 4,096 float64 values through Float64bits in a median 6,511 ns, versus 6,267 ns for pre-existing uint64 bits; both allocate zero bytes. This small storage-loop difference is not evidence of a costly FPU startup or a general end-to-end advantage. Bit reinterpretation preserves values; numeric float-to-int conversion does not. Receipt: results/docs-entropy/float-storage-bench.log.

Anchored delta has a strong size result but the current materializing decoder allocates more scratch than raw decoding. Patched-direct reduces outlier decode allocation from 2,048 to 1,024 bytes per block. Sampling misses useful later documentation structure: 1,714,882 encoded bytes versus 1,559,444 for full-attempt adaptive Zstd. Do not enable it universally. Very short bulk timings have coarse Windows timer resolution; a displayed 0.000 ms is below measurement resolution, not free work. The matrix supports size and allocation decisions more strongly than precise CPU rankings.

## Scope and validation

All new codecs are opt-in test-harness candidates. Production Badger table formats and NornicDB dependencies remain unchanged. Corpus and results manifests contain paths, hashes and measurements rather than copied documentation payloads. Byte-exact round trips, integer overflow edges, malformed framing, float special values, persistence and reopen are checked. Before promotion: representative graph properties, larger blocks, bounded random-access decode, concurrency, encrypted/churn/GC/crash qualification and controlled RSS/CPU measurement.

## Reproduce

```powershell
$env:NORNIC_DOCS_CORPUS='C:/Data/src/NornicDB/docs'
$env:NORNIC_DOCS_RESULTS='C:/temp/nornic-docs-entropy'
go test ./experimental/nornic -run '^TestDocsEntropyExperiment$' -count=1 -v -timeout 30m
# Optional comma-separated filters: NORNIC_DOCS_CODECS, NORNIC_DOCS_SHAPES.
# Repeat three times. The original receipt families retain before/after candidates.
$env:NORNIC_DOCS_SWEEP_RESULTS='C:/temp/nornic-docs-outliers.json'
go test ./experimental/nornic -run '^TestDocsOutlierSweep$' -count=1
go test ./experimental/nornic -run '^$' -bench '^BenchmarkDocsFloatStorage$' -benchmem -benchtime=300ms -count=3 -cpu=1
```
