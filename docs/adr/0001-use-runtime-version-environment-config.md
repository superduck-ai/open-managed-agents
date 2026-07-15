---
status: accepted
---

# Use runtime-version Environment configuration instead of Packages

Cloud Environments use a controlled `config.env_vars` object for neutral `ENV_*_VERSION` runtime selectors, retain a real `init_script`, and remove `config.packages` and the generic `config.environment`; existing values of the removed fields are discarded, while self-hosted Environments remain unchanged. This deliberately breaks strong typing against the current Anthropic SDK `BetaCloudConfig`, because retaining an empty Packages object would falsely advertise package orchestration that the Codex Universal-derived Sandbox does not provide. Runtime selectors are applied only to newly created Sandboxes, and invalid but well-formed versions fail Sandbox initialization against the versions actually installed in the pinned image.
