# Implementation plan — 007 Dashboard visuals & machine identity

[docs](../../../index.md) / [Roadmap](../../index.md) / [007 — Dashboard visuals & machine identity](index.md) / **Implementation plan**

Shipped so far (within the review-bounded scope this item already had —
no new persistence format, identity limited to the allowlist,
data-gathering in `snapshotCache.refresh` not in a `Render`):

- [x] Machine identity as the **System** page — hostname, OS
      name/version, architecture, core count (`info.Collect`). None of
      the fingerprint-grade fields the scope boundary rules out.
- [x] Distinctive overview — severity-coloured resource cards
      (`resourceCard`, coloured by `doctor.AnalyzeResource`) replacing
      the bare verdict banner + text.
- [x] Overview **sparklines** — a 24h CPU / memory / disk trend strip
      per card, read from `doctor.LoadHistory()` in the snapshot
      cache's refresh (`sparkline.go`, `PageContext.History`). Fixed
      0–100 domain so a steady value reads as steady; disk drops the
      "no mount measured" zero sentinel.

Still needs a design doc + review panel before it's built (per
`index.md`'s Trigger):

- [ ] Richer historical charts — a real time axis, hover-to-read
      values, and a per-resource-page history view rather than only the
      Overview thumbnail. Retention window / resolution is the open
      question the review has to settle.
