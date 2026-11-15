# Format-compatible integration branch

`release/projected-reads-v4.9.6` starts from upstream Badger v4.9.6
(`fbd8d2eefad8be8757249767255faf989945b599`). It provides the pinned dependency
for NornicDB's independently reviewed projected-read integration.

This branch changes no Badger runtime code or on-disk format. Projected entity
reads, candidate routing, cache coverage and ownership belong to NornicDB.
No new physical partial-I/O capability or Badger performance gain is claimed.

The experimental property and columnar table codecs remain on `main`. They are
not included in this branch and are not required to enable projected reads.
Normal Badger files remain readable by upstream v4.9.6. No converter is required.

Upstream module path, license and notices are retained. NornicDB uses a pinned
module replacement, not a floating branch dependency.
