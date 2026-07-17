# Fixed runtime environment for interactive and E2B login-shell commands.
case "$(id -u)" in
  0) export HOME=/root ;;
  1000) export HOME=/home/user ;;
  1001) export HOME=/home/claude ;;
esac

export VIRTUAL_ENV=/home/user/.local/share/oma/python
export JAVA_HOME=/opt/java/current
export CARGO_HOME=/home/user/.cargo
export GOBIN=/home/user/go/bin
export GOPROXY=https://goproxy.cn,direct
export UV_DEFAULT_INDEX=https://pypi.tuna.tsinghua.edu.cn/simple
export UV_NO_PROGRESS=1
export BUN_CONFIG_REGISTRY=https://registry.npmmirror.com
export PIP_ROOT_USER_ACTION=ignore
export PIP_CACHE_DIR=/home/claude/.cache/pip
export PIP_CONFIG_FILE=/etc/pip.conf
export PYTHONUNBUFFERED=1
export IS_SANDBOX=yes
export NPM_CONFIG_USERCONFIG=/etc/npmrc
export NODE_PATH=/home/claude/.npm-global/lib/node_modules
export GEM_HOME=/home/user/.local/share/gem
export GEM_PATH=/home/user/.local/share/gem:/opt/ruby/current/lib/ruby/gems/current
export GEMRC=/home/user/.gemrc
export BUNDLE_USER_CONFIG=/home/user/.bundle/config
export MAVEN_HOME=/opt/maven/current
export MAVEN_ARGS="-s /home/user/.m2/settings.xml"
export GRADLE_HOME=/opt/gradle/current
export GRADLE_USER_HOME=/home/user/.gradle
export COMPOSER_HOME=/home/user/.config/composer
export PATH=/home/claude/.npm-global/bin:/home/claude/.local/bin:/home/user/.local/share/oma/python/bin:/home/user/.local/bin:/home/user/go/bin:/home/user/.cargo/bin:/home/user/.local/share/gem/bin:/opt/python/current/bin:/opt/node/current/bin:/opt/go/current/bin:/opt/java/current/bin:/opt/php/current/bin:/opt/bun/bin:/opt/rust/current/bin:/opt/ruby/current/bin:/opt/maven/current/bin:/opt/gradle/current/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
