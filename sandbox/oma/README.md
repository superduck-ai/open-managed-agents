# OMA Sandbox

`oma-sandbox` is the Linux AMD64 execution image used by Open Managed Agents.
It derives its language toolchains from an immutable Universal Runtime image,
then copies only `environment-manager` and the Claude executable from an
immutable donor image. OMA supplies its own entrypoint and offline Runtime
Setup.

## Build

Authenticate Docker to GHCR with a token that can read the private donor
package, then build the AMD64 image:

```bash
docker buildx build \
  --platform linux/amd64 \
  --load \
  --tag oma-sandbox:test \
  sandbox/oma
```

The `DONOR_REPOSITORY` build argument exists for an authenticated registry
mirror. It only replaces the registry/repository prefix; the Dockerfile always
appends the required immutable donor manifest digest.

## Runtime selectors

The image supports these selectors and defaults:

| Selector | Default |
|---|---:|
| `ENV_PYTHON_VERSION` | `3.12` |
| `ENV_NODE_VERSION` | `22` |
| `ENV_RUBY_VERSION` | `3.4.4` |
| `ENV_RUST_VERSION` | `1.89.0` |
| `ENV_GO_VERSION` | `1.25.1` |
| `ENV_BUN_VERSION` | `1.2.14` |
| `ENV_PHP_VERSION` | `8.4` |
| `ENV_JAVA_VERSION` | `21` |
| `ENV_SWIFT_VERSION` | `6.1` |

`setup_runtime.sh` only activates versions already installed in the image. An
unavailable version fails initialization and never triggers a Runtime download.
Platform-owned mise operations run offline without loading workspace config.
Setup persists all selected concrete Runtime paths and `JAVA_HOME` in an OMA
profile that login shells load after upstream manager hooks, so ordinary
entrypoint and standalone Setup flows keep the same versions even inside a
workspace with conflicting manager files.

## Verify

Fast source and script contracts do not require the image:

```bash
sandbox/oma/test.sh
```

Run the container contract after building:

```bash
OMA_SANDBOX_IMAGE=oma-sandbox:test sandbox/oma/test.sh
```

The container contract checks default and explicit Runtime versions, offline
failure, idempotency, root and compatibility accounts, donor executables,
system package mirrors, upstream attribution, and neutral startup output.
