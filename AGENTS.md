## Agent skills

### Issue tracker

Issues are tracked as local markdown files under `.scratch/`. See `docs/agents/issue-tracker.md`.

### Triage labels

The repo uses the default five-label triage vocabulary. See `docs/agents/triage-labels.md`.

### Domain docs

This is a single-context repo using root-level `CONTEXT.md` and `docs/adr/`. See `docs/agents/domain.md`.

---

The project is still in development, we prefer a clean break over adding fallbacks or preserving backward compatibility.

## Ownership conventions

Do not defensively copy slices, maps, or pointers across internal boundaries
unless mutation is part of the contract or a concrete aliasing bug requires
isolation. Treat published values as read-only by convention; passing a slice
does not imply permission to mutate it.
