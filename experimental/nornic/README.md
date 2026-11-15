# Experimental property codecs

This package provides bounded, exact int64 column encoding and per-property S2
compression for NornicDB's opt-in storage branch. The consumer controls new writes
with `NORNICDB_EXPERIMENTAL_PROPERTY_STORAGE_ENABLED`; reading stored frames remains
supported when the flag is disabled. The physical SST block codec is owned by
`table/columnar.go` and `experimental/columnar`, not by a per-entity record adapter.

New property frames use S2 with a declared output length and CRC32C. Legacy Zstd
property frames remain readable. `DecodePayloadInto` reuses caller-owned capacity
without exceeding the declared size; `DecodePayload` returns independent bytes.
SST blocks still choose their own raw, Zstd or transposed-Zstd representation.
The consumer's [regression follow-up](https://github.com/orneryd/NornicDB/blob/experiment/projected-badger-v1.5.0/experiments/property-storage/REGRESSION-FOLLOWUP.md)
records the separate storage, allocation and latency checks.

The unused NPR1 record directory, its `ProjectItem` adapter and its sample column
segment were removed. NPR1 cost 24 bytes per property plus a 12-byte header, never
entered Nornic's runtime path, and did not establish partial physical I/O through
Badger. The document-point regression was in the consumer's projection routing
and caching; retaining a second record format did not help correct it.

Current consumer measurements and correction:
[Nornic indexed point-read report](https://github.com/orneryd/NornicDB/blob/experiment/projected-badger-v1.5.0/experiments/property-storage/POINT-PROJECTION-FIX.md).

Integer encoding chooses constant, frame-of-reference bit packing or raw storage.
Signed extrema and every packed width retain exact int64 values. `DecodeInto`
dispatches once for a batch; `At` reads an individual value. Raw high-entropy values
remain raw when compression does not earn its overhead. No format here is a
supported general-purpose Badger storage contract.

## Focused checks

```sh
go test ./experimental/nornic ./experimental/columnar -count=1
go test -race ./experimental/nornic ./experimental/columnar -count=1
go vet ./experimental/nornic ./experimental/columnar
go test ./experimental/nornic -run '^$' -fuzz '^FuzzDecodeFormats$' -fuzztime=5s -parallel=2
go test ./experimental/nornic -run '^$' -fuzz '^FuzzInt64Column$' -fuzztime=5s -parallel=2
```

Keep timing separate from competing tests. Decode throughput is logical bytes,
not device throughput. Full upstream crash/GC and suite qualification remain
separate work; root-package vet has recorded unchanged upstream protobuf lock-copy
findings in `results/baseline/upstream-vet.log`.

## Historical experiments

- [Batch decoder measurements](BENCHMARK-DIFFERENCE.md)
- [Storage and allocation measurements](STORAGE-MEMORY-REPORT.md)
- [Documentation-seeded entropy experiments](DOCS-ENTROPY-REPORT.md)
- [High-entropy design research](HIGH-ENTROPY-RESEARCH.md)

These reports retain their original revision/workload limits. Removed record
prototype measurements describe historical code, available in Git history.
