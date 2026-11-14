# Handling high-entropy data

## Decision

Prioritize bounded adaptive encoding, smaller exception-aware integer blocks and projected execution. Add string and float codecs only where representative workloads justify them. Truly independent full-width random values cannot generally be reduced losslessly; compensate by avoiding expansion, compression CPU and unnecessary reads or copies.

These are research recommendations, not newly implemented or benchmarked codecs.

## Evidence from this fork

The [storage experiment](STORAGE-MEMORY-REPORT.md) measured approximately 0.05% directory growth for the high-entropy fixture. The raw column fallback still adds a 20-byte header per block. Moving shared metadata into a segment manifest could amortize this cost, but codec, count, integrity and format-version information must remain recoverable.

In the pinned Badger v4.9.6 `table/builder.go`, `handleBlock` invokes the configured compressor and accepts its output without a size-gain check. Compression precedes encryption. A per-block raw bypass would require an explicit compatible format and reader dispatch: existing table-level compression metadata cannot simply describe a mixture of raw and compressed blocks. This is separate from value-log placement and the experimental column codec.

The current fixture repeats a seeded random sequence across blocks with a block-dependent offset. Extend it with independently generated blocks before generalizing to production entropy or compression behavior. Current memory measurements cover query allocations, not whole-process RSS.

## Candidates, in priority order

| Candidate | Helps when | Limits and experiment |
|---|---|---|
| Adaptive raw fallback | Compression produces little gain | Bound candidate selection and scratch memory. Sample to select candidates, then compare actual encoded bytes including metadata. Require a minimum saving. Truly random input should remain raw with minimal overhead. |
| Mini-block FOR and patched exceptions | A few wide integers force the entire current block into raw mode | Compare 64/128/256-value mini-blocks and exception positions/high bits. Include exception metadata and lookup cost. No expected advantage for uniformly random 64-bit values. |
| Delta and delta-of-delta | Large absolute timestamps or IDs have small successive differences | Preserve overflow and exact values. Compare ordered and shuffled fixtures. Sorting needs identity/permutation storage and cannot change observable results or MVCC semantics. |
| Byte/bit shuffle followed by compression | Typed values share bit regions that byte-oriented compression misses | A reversible transform exposes structure; it does not remove entropy. Measure random projection cost because a whole compressed block may still need decoding. |
| FSST or trained string dictionaries | Unique paths, URLs and identifiers share substrings | Whole-value dictionaries miss similar-but-unique strings. Include dictionary size, cold-start and individual-string access. Independent random strings remain poor candidates. |
| Lossless float encoding such as ALP | Decimal-origin floats or correlated bit regions | Verify exact round trips, including negative zero and NaN payload requirements. Quantization changes the data contract and belongs in a separate opt-in experiment. |
| Deduplication or shared immutable payloads | Large incompressible blobs repeat | Count reference/index costs. Verify bytes after hash matches and preserve transaction visibility, tenant isolation and GC safety. Badger already separates large values; prove additional benefit rather than assuming it. |

## Memory and I/O compensation

Even incompressible payloads can be cheaper to query. Keep typed raw buffers instead of expanded property maps; run supported predicates directly over borrowed bytes; use bounded scratch buffers; and read selected properties or pages only. The discarded NPR1 `ProjectItem` adapter used ordinary `Item.Value`; it did not establish physical partial reads. Nornic now projects through its existing typed property format.

Keep compression units independently addressable and benchmark the tradeoff between metadata and excess decoding. Bound concurrent decompressions, cache admission and total scratch bytes. Avoid caching both encoded and expanded copies without measured reuse. Compress before encryption; ciphertext should not be expected to compress.

## Next experiment matrix

1. Independent random int64 blocks, current fixture, monotone/shuffled timestamps, and narrow integers contaminated with 0/0.1/1/5/20% wide outliers.
2. Unique related URLs/paths, repeated strings, random strings, text versus binary UUID representations, precompressed blobs and correlated versus random floating-point bit patterns. Representation changes must preserve the original API value exactly.
3. Compare raw, current FOR, mini-block FOR, patched FOR, delta, shuffle plus Snappy/Zstd, then selected string/float candidates. Do not brute-force every codec on every write.
4. Report total database bytes including dictionaries, metadata and permutations; write and compaction CPU; sequential and projected cold/warm read latency; allocations, peak RSS, cache footprint and scratch under concurrency. Include churn and reopen correctness. Use the same durability and integrity settings for all variants.

Success for independent random data means bounded overhead and competitive CPU/memory, rather than a promised compression ratio. For structured data, accept a codec only when end-to-end savings justify its access and maintenance costs.

## Primary sources

- [DuckDB: Lightweight Compression](https://duckdb.org/2022/10/28/lightweight-compression): adaptive lightweight representations and the limits of random-data compression.
- [FastPFOR implementation](https://github.com/fast-pack/FastPFOR): integer compression techniques to evaluate for exception-heavy blocks.
- [Bitshuffle implementation and algorithm](https://github.com/kiyo-masui/bitshuffle): bit transposition before LZ4/Zstd compression.
- [FSST paper](https://www.vldb.org/pvldb/vol13/p2649-boncz.pdf): substring-based compression with access to individual compressed strings.
- [ALP paper summary and implementation context](https://duckdb.org/library/alp/): adaptive lossless floating-point compression designed for vectorized execution.

These sources establish candidate techniques. Their published results are not performance predictions for NornicDB or this fork.
