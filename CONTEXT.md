# Open Managed Agents Context

## Ubiquitous Language

- **Model Catalog**: The set of models that the configured BYOK provider currently makes available to this installation.
- **Catalog Snapshot**: One complete, successfully discovered view of a Model Catalog. A snapshot is either published as a whole or not published at all.
- **Model Selection**: A model ID intentionally chosen by a user or an installation default for a new operation. It is not inferred from catalog ordering.
- **Stale Catalog**: The most recent successful Catalog Snapshot after a later refresh attempt failed. It remains usable while clearly carrying its stale state.
- **Unknown Model**: A model ID that does not belong to the current Catalog Snapshot and cannot be selected for a new or updated Agent.
- **Historical Model Reference**: A model ID already recorded in an existing Agent version or Session. It remains an immutable record even if it later leaves the Model Catalog.
