# NornicDB storage development

Private development copy of Badger v4.9.6. Upstream history, license and notices are retained. The module path remains upstream's; no NornicDB production dependency has changed.

Read [the implementation plan](PLAN.md) and [prototype status](experimental/nornic/README.md).

The initial experiment isolates addressable property records and compressed integer columns. Range selection in the prototype does not establish physical partial reads through Badger. Engine block reads, live column generations, mutation overlays and NornicDB adaptation are subsequent milestones.
