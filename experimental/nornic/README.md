# Projected records and compressed columns: first prototype

This package begins the [Nornic storage plan](../../PLAN.md). Formats are provisional; no production data or NornicDB dependency has changed.

## Implemented

- Sorted property-token directory with absolute offsets and lengths; opaque encoded values retain their caller-defined types.
- Range-backed projection with cancellation, consumer errors, missing tokens, empty values and callback-scoped borrowed buffers. It does not perform one Badger key lookup per property.
- Bounded int64 columns choosing constant, frame-of-reference bit packing or raw encoding; exact signed extremes and all bit widths; direct row access without expanding neighbors.
- A projected read-only column segment composes the same directory with column payloads.
- Real Badger tests for pending writes, snapshot reads, subsequent writes, reopen and column persistence, using inline and value-log values with plaintext and encryption.

## What is not implemented

Physical range/block reads inside Badger, compressed/encrypted block layout changes, independent integrity units, range coalescing, SSTable partial decompression, GC relocation leases, column identity/presence/type maps, SIMD, live delta generations, compaction, NornicDB consumers and a new public storage format remain planned. Selected property values still require a buffer as large as that selected value. The directory is currently 24 bytes per field plus a 12-byte header; it is an explicit baseline, not a compactness claim for small records.

`ProjectItem` goes through upstream `Item.Value`. The instrumented range-reader tests prove selected *logical payload* access only. They do not prove fewer disk reads or reduced checksum/decryption work in Badger. Column `At` borrows immutable bytes; a projection visitor must copy bytes it retains after the callback.

## Validation

Local environment: Windows/amd64, Intel i9-9900KF, Go 1.25.4, upstream v4.9.6 at fbd8d2eefad8be8757249767255faf989945b599.

- Seven top-level tests and three fuzz seed targets pass; package statement coverage 95.1%.
- Race checks pass, including the plaintext/encrypted inline/value-log integration cases.
- Three bounded two-worker fuzz runs pass: 124,811 malformed-format executions; 117,667 projection executions; 12,514 integer-column executions. These are short exploration runs, not exhaustive validation.
- `go vet ./experimental/nornic` passes.
- `go vet ./experimental/nornic .` reports protobuf lock-copy findings in unchanged upstream publisher.go, db_test.go and publisher_test.go. The combined check is not clean; findings are retained in [results/baseline/upstream-vet.log](results/baseline/upstream-vet.log). No findings were suppressed or fixed outside scope.
- The complete upstream suite, crash/GC qualification, Linux runs and NornicDB consumer tests have not run. Engine behavior remains upstream's; do not promote this prototype.

## Initial measurements

Encoded sizes for 4,096 int64 values, including the 20-byte column header:

| Input shape | Encoding | Bytes |
| --- | --- | ---: |
| Identical values | Constant | 20 |
| Values in a 64-value range | 6-bit frame of reference | 3,092 |
| Increasing 0 through 4,095 | 12-bit frame of reference | 6,164 |
| High entropy spanning signed int64 | Raw | 32,788 |

A raw block with the same header is 32,788 bytes. These are synthetic data shapes, not database-wide compression ratios; strings, identities, nulls, indexes and directories are not included in this column-size table.

The scalar per-value decoder is slower than direct raw decoding. Compression is established for the compressible fixtures; extreme throughput is not. Decoder dispatch/bounds overhead and packed extraction are candidates for the next batch-kernel experiment. Initial concurrently collected timing samples were noisy, so [the final benchmark receipt](results/baseline/bench.log) uses CPU=1, three repetitions and 300 ms cells. These remain local microbenchmarks, not controlled NornicDB comparisons.

The projected-record test reads the 84-byte directory/header and two requested 5-byte payloads while never requesting the unselected 1 MiB payload from its instrumented ReaderAt. The segment test reads only the requested encoded column after opening the directory. Neither claim is physical disk I/O.

## Reproduce

```powershell
go test ./experimental/nornic -count=1 -cover
go test -race ./experimental/nornic -count=1
go vet ./experimental/nornic
go test ./experimental/nornic -run '^$' -fuzz '^FuzzDecodeFormats$' -fuzztime=5s -parallel=2
go test ./experimental/nornic -run '^$' -fuzz '^FuzzRecordProjection$' -fuzztime=5s -parallel=2
go test ./experimental/nornic -run '^$' -fuzz '^FuzzInt64Column$' -fuzztime=5s -parallel=2
go test ./experimental/nornic -run '^$' -bench . -benchmem -benchtime=300ms -count=3 -cpu=1
```

Keep benchmarks separate from competing test/fuzz activity. Go's workload-specific byte throughput is decoded logical bytes, not storage-device throughput. Correctness includes signed extrema, all packed widths, zero-length/absent fields, malformed/truncated formats, callback errors, cancellation, borrowed-buffer use and consistent Badger snapshot selection.
