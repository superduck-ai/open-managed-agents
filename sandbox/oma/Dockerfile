# syntax=docker/dockerfile:1.7

ARG DONOR_REPOSITORY=ghcr.io/superduck-ai/claude-code-interpreter

FROM --platform=linux/amd64 ${DONOR_REPOSITORY}@sha256:37316cf888deea15ca9995abfb3867c8dff24f043a21a7c766fff0e42913753a AS donor

FROM --platform=linux/amd64 ghcr.io/openai/codex-universal@sha256:1641c7bc30b00e0c5d4858b3e4da750123e9802fdb8086e9baa5afa2bc99393c

ARG TARGETARCH

LABEL org.opencontainers.image.title="OMA Sandbox" \
      org.opencontainers.image.description="Open Managed Agents universal Sandbox runtime" \
      org.opencontainers.image.source="https://github.com/superduck-ai/open-managed-agents" \
      org.opencontainers.image.base.name="ghcr.io/openai/codex-universal" \
      org.opencontainers.image.base.digest="sha256:1641c7bc30b00e0c5d4858b3e4da750123e9802fdb8086e9baa5afa2bc99393c" \
      ai.superduck.oma.donor.name="ghcr.io/superduck-ai/claude-code-interpreter" \
      ai.superduck.oma.donor.digest="sha256:37316cf888deea15ca9995abfb3867c8dff24f043a21a7c766fff0e42913753a" \
      ai.superduck.oma.upstream.source="https://github.com/openai/codex-universal/tree/47f4f0eb5337083e2f610db0d15558932cb4901d"

ENV LANG=C.UTF-8 \
    HOME=/root \
    DEBIAN_FRONTEND=noninteractive \
    IS_SANDBOX=yes \
    PIP_ROOT_USER_ACTION=ignore \
    PYTHONUNBUFFERED=1 \
    ENV_PYTHON_VERSION=3.12 \
    ENV_NODE_VERSION=22 \
    ENV_RUBY_VERSION=3.4.4 \
    ENV_RUST_VERSION=1.89.0 \
    ENV_GO_VERSION=1.25.1 \
    ENV_BUN_VERSION=1.2.14 \
    ENV_PHP_VERSION=8.4 \
    ENV_JAVA_VERSION=21 \
    ENV_SWIFT_VERSION=6.1

RUN test "$TARGETARCH" = amd64 \
    && sed -i \
        -e 's|http://archive.ubuntu.com/ubuntu|https://mirrors.tuna.tsinghua.edu.cn/ubuntu|g' \
        -e 's|http://security.ubuntu.com/ubuntu|https://mirrors.tuna.tsinghua.edu.cn/ubuntu|g' \
        /etc/apt/sources.list.d/ubuntu.sources \
    && printf '%s\n' \
        '[global]' \
        'index-url = https://pypi.tuna.tsinghua.edu.cn/simple' \
        > /etc/pip.conf \
    && chmod 0644 /etc/pip.conf \
    && if ! id -u user >/dev/null 2>&1; then useradd -m -s /bin/bash user; fi \
    && if ! id -u claude >/dev/null 2>&1; then useradd -m -s /bin/bash claude; fi \
    && printf '%s\n' 'user ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/user \
    && printf '%s\n' 'claude ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/claude \
    && chmod 0440 /etc/sudoers.d/user /etc/sudoers.d/claude \
    && rm -rf \
        /home/claude/.npm \
        /home/claude/.npm-global \
        /home/claude/.cache/pip \
        /home/claude/.config/pip \
        /opt/entrypoint.sh \
        /opt/codex/setup_universal.sh

COPY --from=donor --chmod=0755 /opt/env-runner/environment-manager /opt/env-runner/environment-manager
COPY --from=donor --chmod=0755 /opt/claude-code/bin/claude /opt/claude-code/bin/claude

RUN ln -sf /opt/env-runner/environment-manager /usr/local/bin/environment-manager

ADD --checksum=sha256:a3957f5993937f14ca56474ea28abdee9a37a999ac3773fbb65315108ba905ad \
    https://raw.githubusercontent.com/openai/codex-universal/47f4f0eb5337083e2f610db0d15558932cb4901d/LICENSES/LICENSE \
    /usr/share/doc/oma-sandbox/upstream/LICENSE
ADD --checksum=sha256:4d68823071b852ed746ca91b3a53b4b1e3f7f90fa52cce5656d8f39e6aa43f9c \
    https://raw.githubusercontent.com/openai/codex-universal/47f4f0eb5337083e2f610db0d15558932cb4901d/LICENSES/codex-universal-image-sbom.md \
    /usr/share/doc/oma-sandbox/upstream/codex-universal-image-sbom.md
ADD --checksum=sha256:9cba85696e30fe12ee68d7e09185dbd5da370b91fc5fc08064ea638288bc6d5b \
    https://raw.githubusercontent.com/openai/codex-universal/47f4f0eb5337083e2f610db0d15558932cb4901d/LICENSES/codex-universal-image-sbom.spdx.json \
    /usr/share/doc/oma-sandbox/upstream/codex-universal-image-sbom.spdx.json

COPY --chmod=0755 entrypoint.sh setup_runtime.sh /opt/oma/

# Keep mise's static shims available without its dynamic chpwd hook. The hook
# would otherwise reapply workspace mise.toml selections after every cd and
# override the platform-selected concrete Runtime paths and JAVA_HOME.
RUN sed -i \
        's|^eval "$(mise activate bash)"$|eval "$(mise activate bash --shims)"|' \
        /etc/profile \
    && grep -Fx 'eval "$(mise activate bash --shims)"' /etc/profile >/dev/null \
    && printf '%s\n' \
        '' \
        '# OMA platform-selected Runtime paths' \
        '[ ! -r /etc/profile.d/oma-runtime.sh ] || . /etc/profile.d/oma-runtime.sh' \
        >> /etc/profile

WORKDIR /root
USER root

ENTRYPOINT ["/opt/oma/entrypoint.sh"]
