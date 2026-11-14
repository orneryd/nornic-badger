# NornicDB storage development

Public development fork of Badger v4.9.6. Upstream history, license and notices are retained. The module path remains upstream's; NornicDB consumes a pinned revision on its default-off experimental integration branch.

Read [the implementation plan](PLAN.md) and [prototype status](experimental/nornic/README.md).

The initial experiment isolates addressable property records and compressed integer columns. Range selection in the prototype does not establish physical partial reads through Badger. Engine block reads, live column generations, mutation overlays and broader NornicDB adaptation are subsequent milestones.

## Current physical integration

NornicDB uses one default-off flag, `NORNICDB_EXPERIMENTAL_PROPERTY_STORAGE_ENABLED`, for property payloads and SST byte columns. A pure application callback annotates property tokens and compact relationship fields. Table creation captures the selected codec; opening existing tables uses their persisted manifest codec even when new experimental writes are disabled.

The encoder compares raw bytes, Zstd and byte-column Zstd, using generic value and supplied schema spans. A Snappy candidate reduced block-encode CPU but lost to Zstd on application-level mixed-workload throughput, so it was not retained. Existing raw/Zstd/column-Zstd frames remain readable. Readers reconstruct exact original SST bytes, including entry offsets and checksums; this is whole-block decoding, not projected physical I/O or a CSR execution engine. Stock upstream Badger does not understand the experimental table codec.

Independent property Zstd uses a 128 KiB window and reuses at most 128 KiB of output scratch per pooled encoder. Accepted bytes are copied into caller ownership; rejected incompressible attempts do not allocate a new input-sized output every time. Column restoration writes directly into the table reader's destination allocation.

The NornicDB branch contains the [public-API concurrency harness](https://github.com/orneryd/NornicDB/tree/experiment/projected-badger-v1.5.0/experiments/property-storage/profile), profiles and results. Priorities are latency/throughput under concurrency, then storage size, then memory. Focused checks cover codec bytes, corruption, snapshots, encryption and reopen; full-suite qualification remains deferred by the maintainer.
