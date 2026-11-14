"""Regenerate the documentation-seeded experiment report from committed receipts."""
import json
import pathlib
import statistics

root = pathlib.Path(__file__).resolve().parent
receipts = root / 'results' / 'docs-entropy'
rows = {}
for family in ('final', 'improved', 'float'):
    for run in range(1, 4):
        for row in json.loads((receipts / ('%s%d' % (family, run)) / 'results.json').read_text()):
            rows.setdefault((row['Shape'], row['Codec']), []).append(row)
def med(key, field):
    return int(statistics.median(row[field] for row in rows[key]))
manifest = json.loads((receipts / 'final1' / 'corpus.json').read_text())
lines = ['# Documentation-seeded mixed-entropy experiment', '',
'## Corpus and method', '',
'Input: NornicDB documentation at commit `df07c94fb7df5bb05a0c854cdaa0b8b6eae8182a` (clean docs directory). The manifest records every Markdown path and SHA-256. Corpus digest: `%s`.' % manifest['CorpusSHA256'], '',
'318 documents: the first 16 sorted paths supply up to 4 KiB each for a shared raw Zstd dictionary and are excluded from all evaluation variants. The remaining 302 documents seed 38,656 generated records. Each record has a stable path-derived identity, document path, status, body excerpt, fixed-epoch timestamp and independent SHA-256-derived nonce. Original documentation is also tested in blocks up to 32 KiB. Numeric columns use 128-value blocks; the separate outlier sweep uses 4,096 values. This is synthetic documentation-derived data, not a production graph snapshot.', '',
'Each shape/codec is stored in its own actual Badger database with identical stable keys, default Snappy/table settings and synchronous writes. Close, sum every regular file, reopen, fetch every key and verify every decoded byte. Dictionary variants also store the shared dictionary in the database and include its uncompressed size in encoded payload totals. File lengths are not filesystem allocated sectors. There is no additional graph index, entity mapping, nullable/type bitmap or mutation overlay in this experiment.', '',
'Results are medians of three local Windows/amd64 runs. Codec CPU timings cover bulk encoding and full materializing decode/verification, not database request latency. Allocations come from separate Go benchmarks and exclude retained codec/dictionary state. Default Badger caches and approximately 1 MiB fixed engine metadata remain present. These are clean-close, warm-process comparisons, not cold-device or compaction/churn results. No whole-process RSS or physical partial-read claim is made.', '',
'## Total database footprint', '',
'Raw means packed bytes stored through normal Badger, including its existing Snappy compression. Best is the smallest measured total directory for this shape, not a proposed universal default.', '',
'| Shape | Raw Badger bytes | Smallest candidate | Candidate Badger bytes | Reduction |',
'|---|---:|---|---:|---:|']
for shape in sorted(set(key[0] for key in rows)):
    base = med((shape, 'raw'), 'DirectoryBytes')
    best = min((key for key in rows if key[0] == shape), key=lambda key: med(key, 'DirectoryBytes'))
    size = med(best, 'DirectoryBytes')
    lines.append('| %s | %s | %s | %s | %.2f%% |' % (shape, format(base, ','), best[1], format(size, ','), 100 * (1-size/base)))
lines += ['', '## Candidate receipts', '',
'Encoded bytes include candidate framing and dictionary bytes, before normal Badger compression. Times are bulk milliseconds for the entire shape; decode includes correctness comparison. Decode B/block is the average allocated bytes per complete materialized block. SIMD, FSST and ALP are not implemented here.', '',
'| Shape | Codec | Encoded bytes | Encode ms | Decode/check ms | Decode B/block |',
'|---|---|---:|---:|---:|---:|']
for key in sorted(rows):
    lines.append('| %s | %s | %s | %.3f | %.3f | %s |' % (key[0],key[1],format(med(key,'EncodedBytes'),','),med(key,'EncodeNS')/1e6,med(key,'DecodeNS')/1e6,format(med(key,'AllocBytes'),',')))
lines += ['', '## Outlier sweep', '',
'This separate test uses a hash of the documentation README to seed independent row values, then spreads wide exceptions across a 4,096-value low-range column. Sizes below are encoded bytes, not total database bytes. Sweep times are diagnostic only and are not included in the throughput tables.', '',
'| Wide values / 4,096 | Existing FOR | Mini-block FOR | Patched packing |', '|---|---:|---:|---:|']
sweep = json.loads((receipts/'outlier-sweep.json').read_text())['Rows']
for count in (0,4,41,205,819,4096):
    selected = {r['Codec']:r['EncodedBytes'] for r in sweep if r['Shape']=='outliers-%d-of-4096'%count}
    lines.append('| %d | %s | %s | %s |' % (count,format(selected['for'],','),format(selected['mini-for'],','),format(selected['patched-for'],',')))
lines += ['', '## Decisions', '',
'- Keep raw fallback for independent hashes and full-range random integers. The sampled Zstd candidate compresses at most 512 bytes before deciding whether to attempt the full payload, then applies an exact 1% minimum-gain check including its one-byte tag. It can miss later structure; this is an intentional CPU/ratio tradeoff, not an entropy proof.',
'- Retain FOR for clean low-range integers. Patched packing helps sparse wide exceptions; mini-blocks only contain the damage when exceptions are sufficiently sparse. Zigzag and exception metadata can make patched packing worse than FOR when there are no exceptions.',
'- Store the first absolute timestamp separately from packed deltas. The initial delta prototype let this anchor contaminate the first mini-block. Anchoring reduces timestamp encoded bytes from 302,000 to 9,664 for these fixtures.',
'- Prefer Zstd candidates for documentation and mixed structured records. A held-out-document dictionary improved documentation footprint further, but its incremental mixed-record benefit is small and it regresses random input. Dictionary selection, residency, format identity and lifecycle remain design work.',
'- Decode patched values directly into the result byte buffer. This preserves identical bytes while removing an intermediate int64 array. Compare patched-for and patched-direct allocations; this does not eliminate the final requested output.',
'- Float bit reinterpretation is lossless and still eight bytes per float64. The decimal-float fixture tests shared bit structure through FOR, shuffle, XOR and general compression. Random float bit patterns are covered by random-int64 and explicit signed-zero, infinity, subnormal and distinct NaN-payload round trips. No numeric cast or quantization is used.',
'- FSST and ALP remain candidates for a later implementation; these results do not substitute Zstd dictionaries for FSST or claim an ALP evaluation. No global sort/permutation or shared-blob deduplication has been introduced. Deduplication needs ownership, visibility and GC design; the source corpus has no identical whole-document hash duplicates.', '',
'## Float storage and remaining tradeoffs', '',
'The separate CPU=1 Go benchmark writes 4,096 float64 values through Float64bits in a median 6,511 ns, versus 6,267 ns for pre-existing uint64 bits; both allocate zero bytes. This small storage-loop difference is not evidence of a costly FPU startup or a general end-to-end advantage. Bit reinterpretation preserves values; numeric float-to-int conversion does not. Receipt: results/docs-entropy/float-storage-bench.log.', '',
'Anchored delta has a strong size result but the current materializing decoder allocates more scratch than raw decoding. Patched-direct reduces outlier decode allocation from 2,048 to 1,024 bytes per block. Sampling misses useful later documentation structure: 1,714,882 encoded bytes versus 1,559,444 for full-attempt adaptive Zstd. Do not enable it universally. Very short bulk timings have coarse Windows timer resolution; a displayed 0.000 ms is below measurement resolution, not free work. The matrix supports size and allocation decisions more strongly than precise CPU rankings.', '',
'## Scope and validation', '',
'All new codecs are opt-in test-harness candidates. Production Badger table formats and NornicDB dependencies remain unchanged. Corpus and results manifests contain paths, hashes and measurements rather than copied documentation payloads. Byte-exact round trips, integer overflow edges, malformed framing, float special values, persistence and reopen are checked. Before promotion: representative graph properties, larger blocks, bounded random-access decode, concurrency, encrypted/churn/GC/crash qualification and controlled RSS/CPU measurement.', '',
'## Reproduce', '', '```powershell',
"$env:NORNIC_DOCS_CORPUS='C:/Data/src/NornicDB/docs'",
"$env:NORNIC_DOCS_RESULTS='C:/temp/nornic-docs-entropy'",
"go test ./experimental/nornic -run '^TestDocsEntropyExperiment$' -count=1 -v -timeout 30m",
'# Optional comma-separated filters: NORNIC_DOCS_CODECS, NORNIC_DOCS_SHAPES.',
'# Repeat three times. The original receipt families retain before/after candidates.',
"$env:NORNIC_DOCS_SWEEP_RESULTS='C:/temp/nornic-docs-outliers.json'",
"go test ./experimental/nornic -run '^TestDocsOutlierSweep$' -count=1",
"go test ./experimental/nornic -run '^$' -bench '^BenchmarkDocsFloatStorage$' -benchmem -benchtime=300ms -count=3 -cpu=1",
'```', '']
(root/'DOCS-ENTROPY-REPORT.md').write_text('\n'.join(lines), encoding='utf-8')

