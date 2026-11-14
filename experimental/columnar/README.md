# Physical column-layout experiment

This package reversibly groups corresponding value bytes across records. A caller can annotate property or metadata spans using `WithColumnarValueLayout`; Badger's table builder translates them to physical block offsets. The callback receives inline logical values only. Value-log pointers remain opaque.

`options.NornicColumnar` compares complete raw, ordinary Zstd, generic value-column and annotated field-column encodings. Only the smallest representation is stored. It preserves original key headers, values, TTLs, user metadata, version bytes, offsets and checksums. Decode restores the exact original block before the existing iterator and checksum verifier run. Compression remains before encryption.

The format is provisional and needs this fork to read its tables. Normal compression settings remain unchanged by default. `WithCompressionSelector` can select the codec for newly built tables from the same application feature flag; existing tables retain their manifest codec. Nornic's runtime flag remains the sole application switch.

Focused tests cover exact block/iterator round trips, encrypted/plaintext reopen, active old snapshots across updates and compaction, metadata/TTL preservation, switching future tables to ordinary compression, native backup/restore, malformed transform frames and bounded ragged groups. Full suites are deferred while integration continues.

This is physical cross-record column compression. Reads currently restore a complete block. Independently addressable compressed columns and physical projected reads remain implementation work. The initial decoder also uses temporary transform storage before copying into the block allocator; peak allocation and CPU costs require measurement before promotion.
