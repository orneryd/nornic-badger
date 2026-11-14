# Nornic storage experiment

## Objective

Reduce NornicDB's property-read, traversal and aggregation costs by combining addressable entity records, compressed column blocks and bounded streaming APIs inside a private Badger development line. Preserve graph semantics, value types, transaction visibility and durability. Performance claims require measurements against current NornicDB and upstream Badger.

## Repository and boundaries

- Private repository: https://github.com/orneryd/nornic-badger
- Upstream: dgraph-io/badger v4.9.6, commit fbd8d2eefad8be8757249767255faf989945b599; retain its history, Apache-2.0 license and notices.
- GitHub public forks cannot be private. This is a standalone private development copy with an upstream remote. Actions are disabled initially so inherited workflows do not execute on push.
- Keep the upstream Go module path initially; NornicDB can consume a pinned private revision through a development-only Go replace. No production dependency change in the first stage.
- Use the TETRA whitepaper as a source of hypotheses, not an implementable specification. No .tetra compatibility or reproduction of undisclosed algorithms is claimed.

## Architecture

Nornic owns property names/tokens, graph identities, types, query dependencies and graph visibility. Badger owns version selection, byte/block access, storage leases, compression, encryption, checksums and relocation. Directories and payloads must resolve through the same visible record version.

Use three cooperating layouts:

1. **Addressable entity records:** a versioned header, sorted property-token directory and encoded property payloads. Directory entries locate a value without walking preceding values. Preserve encoded types; construct the existing generic property map only for selected results.
2. **Compressed column blocks:** bounded batches of one typed property, with presence/type information and stable entity association. Start with exact int64 frame-of-reference and bit packing; measure raw and constant encodings too. Then evaluate delta, run-length and dictionary encodings. Add strings, mixed types, lists, temporal values and unchanged IEEE floating-point representations before general integration.
3. **Compressed adjacency:** grouped outgoing/incoming edge blocks containing target identities, relationship identities and types. Preserve parallel relationships, self-loops, direction and path uniqueness. Sparse-matrix algorithm views may accelerate suitable traversals but cannot replace multigraph identity.

Directories, dictionaries, reverse adjacency and metadata count toward total storage. This design does not claim to eliminate indexes.

## Work sequence

### 1. Establish the private baseline and codec prototype

Pin upstream, preserve provenance and add isolated experimental code. Implement a property-offset record reader, bounded exact-integer column codecs, tests and allocation/throughput benchmarks. Exercise the formats through real Badger transactions and reopen. Distinguish range-reader requests from physical I/O: Item.Value still uses upstream's value-read path at this stage. Measure directory overhead and compact-offset/adaptive-layout candidates on small records as well as large projections.

### 2. Build version-bound block projection in Badger

Add an experimental resolved-value handle whose directory and payload access share one transaction version and explicit lease. Support point, batch and streaming projection through one owner. Coalesce adjacent ranges; propagate cancellation and consumer errors. Specify inline SSTable values, value-log values and pending writes rather than accelerating only one case.

For real I/O savings, evaluate independently compressed and integrity-checked blocks, plus a compact directory that can be read without the payload. Encrypted blocks need independently verifiable read boundaries. Do not weaken whole-record verification to manufacture a speedup. Value-log GC and compaction may relocate bytes without invalidating active readers. Measure page/block amplification, not a promise of exact-byte disk reads.

### 3. Add compressed column segments and operators

Build immutable column segments at a consistent commit boundary. Use generation-local ordinals with explicit stable-identity mappings; never assume recycled numeric IDs keep their meaning. Add projected scans, encoded equality/range filtering, selection vectors and bounded aggregation. Define overflow behavior and widened accumulators before claiming exact arithmetic. Evaluate a scalar reference first, then word-at-a-time/SIMD implementations behind identical contracts. CPU first; GPU only if transfer-inclusive measurements justify it.

### 4. Add mutation and compaction semantics

Keep one authoritative commit order. A committed delta overlay supplies inserts, updates and tombstones over immutable segments; transaction-local changes remain visible to their own transaction. Publish replacement generations atomically and retain old generations until their readers finish. Prove replay, rollback, snapshot consistency, deletes and idempotent recovery. Field-level update deltas are a separate experiment: read projection alone does not reduce whole-record write amplification.

### 5. Adapt NornicDB through existing read owners

Extend existing projected point/stream APIs and relationship reads to consume the new capability. Compile property tokens once per namespace/read scope, including predicate, visibility and policy dependencies. Apply filters before materializing result maps. Preserve database isolation, labels, types, missing values, embeddings, decay/policy visibility, counters, constraints, locks and MVCC. Existing APIs remain usable; any borrowed batch has explicit lifetime and memory bounds. Do not create a parallel query executor.

### 6. Qualify and decide

Compare upstream, directory-only projection, block projection and compressed-column execution with identical data, outputs, durability, encryption and memory limits. Report cold/warm point reads, scans, hub traversals, shortest path, aggregation and mixed read/write workloads. Include the 3.97M-edge workload, Northwind, sparse/wide records, large unrequested text, skew and high-entropy values.

Measure p50/p95/p99, throughput, allocations, peak RSS/heap/resident mappings, physical/logical bytes read, storage including all metadata, write amplification, build/reopen time, compaction and temporary space. Separate codec microbenchmarks from end-to-end results. Prove batch boundaries, backpressure, concurrent updates, encrypted reads, GC, crash/replay, corrupt/truncated input and active-reader compaction.

Initial decision targets are hypotheses: at least 30% less total graph footprint or 2x throughput on a named workload, with identical results and documented regression limits. No promotion based only on compression ratio or warm-cache microbenchmarks. Production adoption requires final-head Badger qualification, Nornic consumer tests, both parser modes and recovery/replication checks.

## Stop conditions

Reject an approach that needs mixed-version reads, changes public value semantics, bypasses constraint/durability owners, unbounds memory, or wins only by omitting equivalent work. Keep upstream changes reviewable and pin revisions. Maintain benchmark receipts and explicitly record unsupported cases.

## First-stage status

Repository created privately and pinned. Prototype implementation and initial validation are recorded in experimental/nornic/README.md. Later stages above are planned, not implemented or production-ready.
