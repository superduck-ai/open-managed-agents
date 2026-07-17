# Fixed runtime environment for interactive and E2B login-shell commands.
case "$(id -u)" in
  0) export HOME=/root ;;
  1000) export HOME=/home/user ;;
  1001) export HOME=/home/claude ;;
esac

export VIRTUAL_ENV=/home/user/.local/share/oma/python
export JAVA_HOME=/opt/java/25.0.3+9
export CARGO_HOME=/home/user/.cargo
export GOBIN=/home/user/go/bin
export GOPROXY=https://goproxy.cn,direct
export UV_DEFAULT_INDEX=https://pypi.tuna.tsinghua.edu.cn/simple
export UV_NO_PROGRESS=1
export PIP_ROOT_USER_ACTION=ignore
export PIP_CACHE_DIR=/home/claude/.cache/pip
export PIP_CONFIG_FILE=/etc/pip.conf
export PYTHONUNBUFFERED=1
export IS_SANDBOX=yes
export NPM_CONFIG_USERCONFIG=/etc/npmrc
export NODE_PATH=/home/claude/.npm-global/lib/node_modules
export GEM_HOME=/home/user/.local/share/gem
export GEM_PATH=/home/user/.local/share/gem:/opt/ruby/3.4.10/lib/ruby/gems/3.4.0
export GEMRC=/home/user/.gemrc
export MAVEN_HOME=/opt/maven/3.9.11
export MAVEN_ARGS="-s /home/user/.m2/settings.xml"
export GRADLE_HOME=/opt/gradle/9.2.1
export GRADLE_USER_HOME=/home/user/.gradle
export COMPOSER_HOME=/home/user/.config/composer
export PATH=/home/claude/.npm-global/bin:/home/claude/.local/bin:/home/user/.local/share/oma/python/bin:/home/user/.local/bin:/home/user/go/bin:/home/user/.cargo/bin:/home/user/.local/share/gem/bin:/opt/python/3.13.14/bin:/opt/node/24.18.0/bin:/opt/go/1.26.5/bin:/opt/java/25.0.3+9/bin:/opt/php/8.5.8/bin:/opt/bun/bin:/opt/rust/1.97.0/bin:/opt/ruby/3.4.10/bin:/opt/maven/3.9.11/bin:/opt/gradle/9.2.1/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
