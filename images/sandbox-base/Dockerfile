# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e

### BASE IMAGE ###

ARG SANDBOX_PLATFORM
ARG UBUNTU_DIGEST
FROM --platform=${SANDBOX_PLATFORM} ubuntu@${UBUNTU_DIGEST} AS base

ARG DEBIAN_FRONTEND=noninteractive
ARG CA_BUNDLE_URL
ARG CA_BUNDLE_SHA256
ENV LANG=C.UTF-8

# Ubuntu's minimal image does not include a CA bundle. Bootstrap one through
# BuildKit with a content checksum, then install Ubuntu's managed bundle from
# the configured HTTPS mirror. Disable docker-clean so later cache mounts keep
# package archives outside the resulting image instead of serializing empty
# caches across the source-build stages.
ADD --checksum=${CA_BUNDLE_SHA256} \
    ${CA_BUNDLE_URL} /etc/ssl/certs/ca-certificates.crt

RUN chmod 0644 /etc/ssl/certs/ca-certificates.crt \
    && rm -f /etc/apt/apt.conf.d/docker-clean \
    && printf '%s\n' 'Binary::apt::APT::Keep-Downloaded-Packages "true";' > /etc/apt/apt.conf.d/keep-cache \
    && sed -i \
      -e 's|http://archive.ubuntu.com/ubuntu/|https://mirrors.tuna.tsinghua.edu.cn/ubuntu/|g' \
      -e 's|http://security.ubuntu.com/ubuntu/|https://mirrors.tuna.tsinghua.edu.cn/ubuntu/|g' \
      /etc/apt/sources.list.d/ubuntu.sources \
    && apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl xz-utils unzip \
    && rm -rf /var/lib/apt/lists/* /var/cache/apt/archives/*

### SOURCE-BUILT RUNTIMES ###

# Keep expensive source builds in independent stages so BuildKit can reuse each
# runtime without carrying compilers, source trees, or archives into the image.

### PYTHON ###

FROM base AS python_builder
ARG PYTHON_VERSION
ARG PYTHON_SHA256
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update \
    && apt-get install -y --no-install-recommends \
      build-essential libbz2-dev libffi-dev libgdbm-dev liblzma-dev \
      libncurses-dev libreadline-dev libsqlite3-dev libssl-dev tk-dev uuid-dev zlib1g-dev \
    && curl -fsSL --retry 5 --retry-all-errors --connect-timeout 15 \
      "https://www.python.org/ftp/python/${PYTHON_VERSION}/Python-${PYTHON_VERSION}.tar.xz" \
      -o /tmp/python.tar.xz \
    && echo "${PYTHON_SHA256}  /tmp/python.tar.xz" | sha256sum --check --strict \
    && mkdir -p /tmp/python-src \
    && tar -xJf /tmp/python.tar.xz -C /tmp/python-src --strip-components=1 \
    && cd /tmp/python-src \
    && ./configure \
      --prefix="/opt/python/${PYTHON_VERSION}" \
      --enable-optimizations \
      --enable-shared \
      --with-ensurepip=install \
      --with-lto \
      --without-static-libpython \
    && make -j"$(nproc)" \
    && make install \
    && find "/opt/python/${PYTHON_VERSION}" -type d -name __pycache__ -prune -exec rm -rf '{}' + \
    && find "/opt/python/${PYTHON_VERSION}" -type f -name '*.py[co]' -delete

### PHP ###

FROM base AS php_builder
ARG PHP_VERSION
ARG PHP_SHA256
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update \
    && apt-get install -y --no-install-recommends \
      autoconf bison build-essential pkg-config re2c \
      libcurl4-openssl-dev libffi-dev libicu-dev libonig-dev libpq-dev \
      libreadline-dev libsqlite3-dev libssl-dev libxml2-dev libxslt1-dev \
      libzip-dev zlib1g-dev \
    && curl -fsSL --retry 5 --retry-all-errors --connect-timeout 15 "https://www.php.net/distributions/php-${PHP_VERSION}.tar.xz" -o /tmp/php.tar.xz \
    && echo "${PHP_SHA256}  /tmp/php.tar.xz" | sha256sum --check --strict \
    && mkdir -p /tmp/php-src \
    && tar -xJf /tmp/php.tar.xz -C /tmp/php-src --strip-components=1 \
    && cd /tmp/php-src \
    && ./configure \
      --prefix="/opt/php/${PHP_VERSION}" \
      --with-config-file-path=/etc/php \
      --disable-cgi \
      --disable-phpdbg \
      --enable-bcmath \
      --enable-intl \
      --enable-mbstring \
      --enable-opcache \
      --enable-pcntl \
      --enable-sockets \
      --with-curl \
      --with-openssl \
      --with-pdo-mysql \
      --with-pdo-pgsql \
      --with-pdo-sqlite \
      --with-readline \
      --with-sqlite3 \
      --with-zip \
      --with-zlib \
    && make -j"$(nproc)" \
    && make install \
    && strip --strip-unneeded "/opt/php/${PHP_VERSION}/bin/php"

### RUBY ###

FROM base AS ruby_builder
ARG RUBY_VERSION
ARG RUBY_SERIES
ARG RUBY_SHA256
RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update \
    && apt-get install -y --no-install-recommends \
      autoconf build-essential libffi-dev libgdbm-dev libreadline-dev \
      libssl-dev libyaml-dev zlib1g-dev \
    && curl -fsSL --retry 5 --retry-all-errors --connect-timeout 15 \
      "https://cache.ruby-lang.org/pub/ruby/${RUBY_SERIES}/ruby-${RUBY_VERSION}.tar.xz" \
      -o /tmp/ruby.tar.xz \
    && echo "${RUBY_SHA256}  /tmp/ruby.tar.xz" | sha256sum --check --strict \
    && mkdir -p /tmp/ruby-src \
    && tar -xJf /tmp/ruby.tar.xz -C /tmp/ruby-src --strip-components=1 \
    && cd /tmp/ruby-src \
    && ./configure --prefix="/opt/ruby/${RUBY_VERSION}" --disable-install-doc --enable-shared \
    && make -j"$(nproc)" \
    && make install \
    && find "/opt/ruby/${RUBY_VERSION}" -type f \( -name '*.a' -o -name '*.c' \) -delete

### PREBUILT RUNTIME ASSETS ###

# Download each fixed runtime in its own stage and accept it only after its
# repository-pinned checksum matches.

### NODE.JS ###

FROM base AS node_asset
ARG NODE_VERSION
ARG NODE_SHA256
RUN curl -fsSL --retry 5 --retry-all-errors --connect-timeout 15 \
      "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-x64.tar.xz" \
      -o /tmp/node.tar.xz \
    && echo "${NODE_SHA256}  /tmp/node.tar.xz" | sha256sum --check --strict \
    && mkdir -p "/opt/node/${NODE_VERSION}" \
    && tar -xJf /tmp/node.tar.xz -C "/opt/node/${NODE_VERSION}" --strip-components=1 \
    && rm -rf "/opt/node/${NODE_VERSION}/share/doc"

### GO ###

FROM base AS go_asset
ARG GO_VERSION
ARG GO_SHA256
RUN curl -fsSL --retry 5 --retry-all-errors --connect-timeout 15 "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz \
    && echo "${GO_SHA256}  /tmp/go.tar.gz" | sha256sum --check --strict \
    && mkdir -p "/opt/go/${GO_VERSION}" \
    && tar -xzf /tmp/go.tar.gz -C "/opt/go/${GO_VERSION}" --strip-components=1 \
    && rm -rf "/opt/go/${GO_VERSION}/api" "/opt/go/${GO_VERSION}/doc" "/opt/go/${GO_VERSION}/test"

### JAVA ###

FROM base AS java_asset
ARG JAVA_VERSION
ARG JAVA_RELEASE_TAG
ARG JAVA_ARCHIVE
ARG JAVA_SHA256
RUN curl -fsSL --retry 5 --retry-all-errors --connect-timeout 15 \
      "https://github.com/adoptium/temurin25-binaries/releases/download/${JAVA_RELEASE_TAG}/${JAVA_ARCHIVE}" \
      -o /tmp/java.tar.gz \
    && echo "${JAVA_SHA256}  /tmp/java.tar.gz" | sha256sum --check --strict \
    && mkdir -p "/opt/java/${JAVA_VERSION}" \
    && tar -xzf /tmp/java.tar.gz -C "/opt/java/${JAVA_VERSION}" --strip-components=1 \
    && rm -rf "/opt/java/${JAVA_VERSION}/demo" "/opt/java/${JAVA_VERSION}/man"

### BUN ###

FROM base AS bun_asset
ARG BUN_VERSION
ARG BUN_SHA256
RUN curl -fsSL --retry 5 --retry-all-errors --connect-timeout 15 \
      "https://github.com/oven-sh/bun/releases/download/bun-v${BUN_VERSION}/bun-linux-x64-baseline.zip" \
      -o /tmp/bun.zip \
    && echo "${BUN_SHA256}  /tmp/bun.zip" | sha256sum --check --strict \
    && unzip -q /tmp/bun.zip -d /tmp/bun \
    && install -Dm0755 /tmp/bun/bun-linux-x64-baseline/bun /opt/bun/bin/bun \
    && ln -s bun /opt/bun/bin/bunx

### RUST ###

FROM base AS rust_asset
ARG RUST_VERSION
ARG RUST_TARGET
ARG RUST_SHA256
RUN curl -fsSL --retry 5 --retry-all-errors --connect-timeout 15 \
      "https://static.rust-lang.org/dist/rust-${RUST_VERSION}-${RUST_TARGET}.tar.xz" \
      -o /tmp/rust.tar.xz \
    && echo "${RUST_SHA256}  /tmp/rust.tar.xz" | sha256sum --check --strict \
    && mkdir -p /tmp/rust \
    && tar -xJf /tmp/rust.tar.xz -C /tmp/rust --strip-components=1 \
    && /tmp/rust/install.sh \
      --prefix="/opt/rust/${RUST_VERSION}" \
      --disable-ldconfig \
      --components="rustc,rust-std-${RUST_TARGET},cargo,rustfmt-preview,clippy-preview,rust-analyzer-preview,llvm-tools-preview" \
    && rm -rf "/opt/rust/${RUST_VERSION}/share/doc"

### DEVELOPMENT TOOL ASSETS ###

# Collect uv, Maven, Gradle, the Composer installer, and the Docker CLI for the
# final runtime stage without retaining their downloaded archives.

FROM base AS tool_assets
ARG UV_VERSION
ARG UV_SHA256
ARG MAVEN_VERSION
ARG MAVEN_SHA512
ARG GRADLE_VERSION
ARG GRADLE_SHA256
ARG COMPOSER_VERSION
ARG COMPOSER_INSTALLER_SHA384
ARG DOCKER_CLI_VERSION
ARG DOCKER_CLI_SHA256
RUN curl -fsSL --retry 5 --retry-all-errors --connect-timeout 15 \
      "https://github.com/astral-sh/uv/releases/download/${UV_VERSION}/uv-x86_64-unknown-linux-gnu.tar.gz" \
      -o /tmp/uv.tar.gz \
    && echo "${UV_SHA256}  /tmp/uv.tar.gz" | sha256sum --check --strict \
    && tar -xzf /tmp/uv.tar.gz -C /tmp \
    && install -Dm0755 /tmp/uv-x86_64-unknown-linux-gnu/uv /opt/tools/bin/uv \
    && install -Dm0755 /tmp/uv-x86_64-unknown-linux-gnu/uvx /opt/tools/bin/uvx \
    && curl -fsSL --retry 5 --retry-all-errors --connect-timeout 15 \
      "https://archive.apache.org/dist/maven/maven-3/${MAVEN_VERSION}/binaries/apache-maven-${MAVEN_VERSION}-bin.tar.gz" \
      -o /tmp/maven.tar.gz \
    && echo "${MAVEN_SHA512}  /tmp/maven.tar.gz" | sha512sum --check --strict \
    && mkdir -p "/opt/maven/${MAVEN_VERSION}" \
    && tar -xzf /tmp/maven.tar.gz -C "/opt/maven/${MAVEN_VERSION}" --strip-components=1 \
    && rm -rf "/opt/maven/${MAVEN_VERSION}/lib/ext" \
    && curl -fsSL --retry 5 --retry-all-errors --connect-timeout 15 \
      "https://services.gradle.org/distributions/gradle-${GRADLE_VERSION}-bin.zip" \
      -o /tmp/gradle.zip \
    && echo "${GRADLE_SHA256}  /tmp/gradle.zip" | sha256sum --check --strict \
    && unzip -q /tmp/gradle.zip -d /tmp \
    && mkdir -p "/opt/gradle/${GRADLE_VERSION}" \
    && cp -a "/tmp/gradle-${GRADLE_VERSION}/." "/opt/gradle/${GRADLE_VERSION}/" \
    && curl -fsSL --retry 5 --retry-all-errors --connect-timeout 15 https://getcomposer.org/installer -o /tmp/composer-setup.php \
    && echo "${COMPOSER_INSTALLER_SHA384}  /tmp/composer-setup.php" | sha384sum --check --strict \
    && mkdir -p /opt/composer \
    && curl -fsSL --retry 5 --retry-all-errors --connect-timeout 15 \
      "https://download.docker.com/linux/static/stable/x86_64/docker-${DOCKER_CLI_VERSION}.tgz" \
      -o /tmp/docker.tgz \
    && echo "${DOCKER_CLI_SHA256}  /tmp/docker.tgz" | sha256sum --check --strict \
    && tar -xzf /tmp/docker.tgz -C /tmp \
    && install -Dm0755 /tmp/docker/docker /opt/tools/bin/docker

### CLAUDE CODE ###

FROM base AS claude_asset
ARG CLAUDE_VERSION
ARG CLAUDE_SHA256
RUN curl -fsSL --retry 5 --retry-all-errors --connect-timeout 15 \
      "https://registry.npmjs.org/@anthropic-ai/claude-code-linux-x64/-/claude-code-linux-x64-${CLAUDE_VERSION}.tgz" \
      -o /tmp/claude.tgz \
    && echo "${CLAUDE_SHA256}  /tmp/claude.tgz" | sha256sum --check --strict \
    && mkdir -p /tmp/claude \
    && tar -xzf /tmp/claude.tgz -C /tmp/claude \
    && install -Dm0755 /tmp/claude/package/claude /opt/claude-code/bin/claude

### ENVIRONMENT MANAGER ###

# The independent environment-manager release is supplied as a named build
# context. scripts/sandbox-image.sh verifies its repository-pinned SHA-256 before
# invoking BuildKit; the final stage verifies it again after COPY.
FROM environment_manager AS environment_manager_source

### FINAL RUNTIME IMAGE ###

FROM base AS runtime

### RUNTIME CONTRACT AND ENVIRONMENT ###

ARG SANDBOX_PLATFORM
ARG UBUNTU_DIGEST
ARG RUNTIME_REVISION
ARG CODEX_UNIVERSAL_RECIPE_REVISION
ARG PYTHON_VERSION
ARG NODE_VERSION
ARG GO_VERSION
ARG JAVA_VERSION
ARG PHP_VERSION
ARG GCC_VERSION
ARG GCC_MAJOR
ARG GCC_APT_VERSION
ARG BUN_VERSION
ARG RUST_VERSION
ARG RUST_TARGET
ARG RUBY_VERSION
ARG CLAUDE_VERSION
ARG YARN_VERSION
ARG PNPM_VERSION
ARG MAVEN_VERSION
ARG GRADLE_VERSION
ARG COMPOSER_VERSION
ARG ENVIRONMENT_MANAGER_REVISION
ARG ENVIRONMENT_MANAGER_VERSION
ENV HOME=/home/user \
    VIRTUAL_ENV=/home/user/.local/share/oma/python \
    JAVA_HOME=/opt/java/current \
    CARGO_HOME=/home/user/.cargo \
    GOBIN=/home/user/go/bin \
    GOPROXY=https://goproxy.cn,direct \
    UV_DEFAULT_INDEX=https://pypi.tuna.tsinghua.edu.cn/simple \
    UV_NO_PROGRESS=1 \
    NPM_CONFIG_USERCONFIG=/etc/npmrc \
    BUN_CONFIG_REGISTRY=https://registry.npmmirror.com \
    NODE_PATH=/home/user/.local/lib/node_modules \
    GEM_HOME=/home/user/.local/share/gem \
    GEM_PATH=/home/user/.local/share/gem:/opt/ruby/current/lib/ruby/gems/current \
    GEMRC=/home/user/.gemrc \
    BUNDLE_USER_CONFIG=/home/user/.bundle/config \
    MAVEN_HOME=/opt/maven/current \
    MAVEN_ARGS="-s /home/user/.m2/settings.xml" \
    GRADLE_HOME=/opt/gradle/current \
    GRADLE_USER_HOME=/home/user/.gradle \
    COMPOSER_HOME=/home/user/.config/composer \
    PATH=/home/user/.local/share/oma/python/bin:/home/user/.local/bin:/home/user/go/bin:/home/user/.cargo/bin:/home/user/.local/share/gem/bin:/opt/python/current/bin:/opt/node/current/bin:/opt/go/current/bin:/opt/java/current/bin:/opt/php/current/bin:/opt/bun/bin:/opt/rust/current/bin:/opt/ruby/current/bin:/opt/maven/current/bin:/opt/gradle/current/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

### SYSTEM PACKAGES AND FIXED COMPILER ###

RUN --mount=type=cache,target=/var/cache/apt,sharing=locked \
    --mount=type=cache,target=/var/lib/apt,sharing=locked \
    apt-get update \
    && apt-get install -y --no-install-recommends \
      gcc-${GCC_MAJOR}=${GCC_APT_VERSION} g++-${GCC_MAJOR}=${GCC_APT_VERSION} \
      ca-certificates cmake curl default-libmysqlclient-dev diffutils gawk git htop jq \
      libc6-dev libbz2-dev libcurl4-openssl-dev libffi-dev libgdbm-dev libicu-dev \
      liblzma-dev libncurses-dev libonig-dev libpq-dev libreadline-dev libsqlite3-dev \
      libssl-dev libxml2-dev libxslt1-dev libyaml-dev libzip-dev make nano openssh-client \
      patch pkg-config postgresql-client redis-tools ripgrep screen sqlite3 sudo tar tmux \
      tree unzip vim wget zip zlib1g-dev \
    && ln -sf "/usr/bin/gcc-${GCC_MAJOR}" /usr/local/bin/gcc \
    && ln -sf "/usr/bin/gcc-${GCC_MAJOR}" /usr/local/bin/cc \
    && ln -sf "/usr/bin/g++-${GCC_MAJOR}" /usr/local/bin/g++ \
    && ln -sf "/usr/bin/g++-${GCC_MAJOR}" /usr/local/bin/c++

### RUNTIME AND TOOL ARTIFACTS ###

COPY --from=python_builder /opt/python/ /opt/python/
COPY --from=php_builder /opt/php/ /opt/php/
COPY --from=ruby_builder /opt/ruby/ /opt/ruby/
COPY --from=node_asset /opt/node/ /opt/node/
COPY --from=go_asset /opt/go/ /opt/go/
COPY --from=java_asset /opt/java/ /opt/java/
COPY --from=bun_asset /opt/bun/ /opt/bun/
COPY --from=rust_asset /opt/rust/ /opt/rust/
COPY --from=tool_assets /opt/tools/bin/ /usr/local/bin/
COPY --from=tool_assets /opt/maven/ /opt/maven/
COPY --from=tool_assets /opt/gradle/ /opt/gradle/
COPY --from=tool_assets /tmp/composer-setup.php /tmp/composer-setup.php
COPY --from=claude_asset /opt/claude-code/ /opt/claude-code/
COPY --from=environment_manager_source /environment-manager /opt/env-runner/environment-manager

### PACKAGE MANAGER, SUDO, AND SHELL CONFIGURATION ###

COPY config/pip.conf /etc/pip.conf
COPY config/npmrc /etc/npmrc
COPY config/sudoers-user /etc/sudoers.d/user
COPY config/sudoers-claude /etc/sudoers.d/claude
COPY config/cargo-config.toml /home/user/.cargo/config.toml
COPY config/maven-settings.xml /home/user/.m2/settings.xml
COPY config/gradle-init.gradle /home/user/.gradle/init.gradle
COPY config/gemrc /home/user/.gemrc
COPY config/bundler-config /home/user/.bundle/config
COPY config/composer-config.json /home/user/.config/composer/config.json
COPY verify-contract.sh /usr/local/bin/verify-sandbox-contract
COPY config/profile.sh /etc/profile.d/oma-sandbox.sh
COPY versions.env /etc/oma-sandbox-versions.env

### RUNTIME LINKS, USERS, AND SHARED DIRECTORIES ###

ARG ENVIRONMENT_MANAGER_SHA256
RUN test -n "$ENVIRONMENT_MANAGER_SHA256" \
    && echo "${ENVIRONMENT_MANAGER_SHA256}  /opt/env-runner/environment-manager" | sha256sum --check --strict \
    && chmod 0440 /etc/sudoers.d/user /etc/sudoers.d/claude \
    && chmod 0644 /etc/profile.d/oma-sandbox.sh \
    && chmod 0755 /opt/env-runner/environment-manager /usr/local/bin/verify-sandbox-contract \
    && ln -s /opt/env-runner/environment-manager /usr/local/bin/environment-manager \
    && ln -s /opt/claude-code/bin/claude /usr/local/bin/claude \
    && ln -s "${PYTHON_VERSION}" /opt/python/current \
    && ln -s "${NODE_VERSION}" /opt/node/current \
    && ln -s "${GO_VERSION}" /opt/go/current \
    && ln -s "${JAVA_VERSION}" /opt/java/current \
    && ln -s "${PHP_VERSION}" /opt/php/current \
    && ln -s "${RUST_VERSION}" /opt/rust/current \
    && ln -s "${RUBY_VERSION}" /opt/ruby/current \
    && ln -s "${MAVEN_VERSION}" /opt/maven/current \
    && ln -s "${GRADLE_VERSION}" /opt/gradle/current \
    && printf '%s\n' /opt/python/current/lib /opt/ruby/current/lib > /etc/ld.so.conf.d/oma-runtimes.conf \
    && ldconfig \
    && ruby_api_version="$(/opt/ruby/current/bin/ruby -rrbconfig -e 'print RbConfig::CONFIG["ruby_version"]')" \
    && ln -s "$ruby_api_version" /opt/ruby/current/lib/ruby/gems/current \
    && cp -a /home/ubuntu/. /home/user/ \
    && usermod --login user --home /home/user --shell /bin/bash ubuntu \
    && groupmod --new-name user ubuntu \
    && rm -rf /home/ubuntu \
    && groupadd --gid 1001 claude \
    && useradd --uid 1001 --gid claude --create-home --shell /bin/bash claude \
    && usermod --append --groups claude user \
    && usermod --append --groups user claude \
    && mkdir -p \
      /root/.claude \
      /home/claude/.cache/pip \
      /home/claude/.claude \
      /home/claude/.local/bin \
      /home/claude/.npm \
      /home/claude/.npm-global/bin \
      /home/claude/.npm-global/lib/node_modules \
      /home/claude/project \
      /home/user/.local/bin \
      /home/user/.local/lib/node_modules \
      /home/user/.local/share/gem \
      /home/user/.local/share/oma \
      /home/user/go/bin \
    && printf '{}\n' > /root/.claude.json \
    && chmod 0700 /root/.claude \
    && chmod 0600 /root/.claude.json \
    && chown -R claude:claude /home/claude \
    && chown -R user:user /home/user \
    && chmod 2770 /home/claude /home/user \
    && chmod 2775 \
      /home/claude/.cache \
      /home/claude/.cache/pip \
      /home/claude/.claude \
      /home/claude/.local \
      /home/claude/.local/bin \
      /home/claude/.npm \
      /home/claude/.npm-global \
      /home/claude/.npm-global/bin \
      /home/claude/.npm-global/lib \
      /home/claude/.npm-global/lib/node_modules \
      /home/claude/project \
    && printf '%s\n' \
      'GOPROXY=https://goproxy.cn,direct' \
      'UV_DEFAULT_INDEX=https://pypi.tuna.tsinghua.edu.cn/simple' \
      'NPM_CONFIG_USERCONFIG=/etc/npmrc' \
      'BUN_CONFIG_REGISTRY=https://registry.npmmirror.com' \
      'PIP_CACHE_DIR=/home/claude/.cache/pip' \
      'PIP_CONFIG_FILE=/etc/pip.conf' \
      'IS_SANDBOX=yes' \
      'NODE_PATH=/home/claude/.npm-global/lib/node_modules' \
      'GEMRC=/home/user/.gemrc' \
      'BUNDLE_USER_CONFIG=/home/user/.bundle/config' \
      'MAVEN_ARGS=-s /home/user/.m2/settings.xml' \
      'GRADLE_USER_HOME=/home/user/.gradle' \
      'COMPOSER_HOME=/home/user/.config/composer' \
      >> /etc/environment \
    && "/opt/php/${PHP_VERSION}/bin/php" /tmp/composer-setup.php \
      --quiet \
      --version="${COMPOSER_VERSION}" \
      --install-dir=/usr/local/bin \
      --filename=composer \
    && rm -f /tmp/composer-setup.php

### CLAUDE-COMPATIBLE PROCESS ENVIRONMENT ###

ENV PIP_ROOT_USER_ACTION=ignore \
    PIP_CACHE_DIR=/home/claude/.cache/pip \
    PIP_CONFIG_FILE=/etc/pip.conf \
    PYTHONUNBUFFERED=1 \
    IS_SANDBOX=yes \
    NODE_PATH=/home/claude/.npm-global/lib/node_modules \
    PATH=/home/claude/.npm-global/bin:/home/claude/.local/bin:/opt/ruby/current/bin:${PATH}

### USER-SCOPED TOOL INITIALIZATION ###

USER user
WORKDIR /home/user

RUN "/opt/python/${PYTHON_VERSION}/bin/python3" -m venv "$VIRTUAL_ENV" \
    && npm install --global --ignore-scripts "yarn@${YARN_VERSION}" "pnpm@${PNPM_VERSION}" \
    && npm cache clean --force \
    && rm -rf \
      /home/user/.cache \
      /home/claude/.npm/_cacache \
      /home/claude/.npm/_logs \
      /home/user/.local/share/pnpm/store \
      /home/user/.gem/specs \
      /home/user/.gradle/caches \
      /home/user/.m2/repository

### FINAL IDENTITY AND IMAGE METADATA ###

USER root
ENV HOME=/root

LABEL org.opencontainers.image.title="Open Managed Agents Sandbox Base" \
      org.opencontainers.image.description="Fixed-runtime linux/amd64 base for E2B managed-agent sandboxes" \
      org.opencontainers.image.base.digest="${UBUNTU_DIGEST}" \
      ai.superduck.oma.sandbox.platform="${SANDBOX_PLATFORM}" \
      ai.superduck.oma.sandbox.runtime-revision="${RUNTIME_REVISION}" \
      ai.superduck.oma.sandbox.recipe-revision="${CODEX_UNIVERSAL_RECIPE_REVISION}" \
      ai.superduck.oma.sandbox.claude-version="${CLAUDE_VERSION}" \
      ai.superduck.oma.sandbox.environment-manager-revision="${ENVIRONMENT_MANAGER_REVISION}" \
      ai.superduck.oma.sandbox.environment-manager-version="${ENVIRONMENT_MANAGER_VERSION}"

### DEFAULT COMMAND ###

CMD ["sleep", "infinity"]
