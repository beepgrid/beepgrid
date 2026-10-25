# Agent icon sources

This sprite contains twelve Lucide outline symbols and twelve Phosphor duotone symbols. Only the used symbols are vendored. No icon framework, third-party image request, remote avatar lookup or runtime package is added.

`manifest.json` records pinned upstream commits, source URLs and original SVG SHA-256 values. `sprite_sha256` covers the assembled sprite. SVG view boxes are retained; root dimensions become symbol dimensions. Lucide stroke width is normalized to 1.65 for the reference style. License notices ship beside the sprite and in the embedded application.

- Lucide: https://github.com/lucide-icons/lucide, ISC plus MIT for Feather-derived icons. See `lucide-LICENSE.txt`.
- Phosphor: https://github.com/phosphor-icons/core, MIT. See `phosphor-LICENSE.txt`.

The geometric marks in `src/AgentMark.tsx` are original BeepGrid vectors. All pack choices are visual identities, not authority or capability labels. Existing immutable record IDs determine the index consistently across views; names remain authoritative because finite symbols can repeat.
