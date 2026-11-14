# NornicDB storage development

Public development fork of Badger v4.9.6. Upstream history, license and notices are retained. The module path remains upstream's; NornicDB consumes a pinned revision on its default-off experimental integration branch.

Read [the implementation plan](PLAN.md) and [prototype status](experimental/nornic/README.md).

The initial experiment isolates addressable property records and compressed integer columns. Range selection in the prototype does not establish physical partial reads through Badger. Engine block reads, live column generations, mutation overlays and broader NornicDB adaptation are subsequent milestones.
