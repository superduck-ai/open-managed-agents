#!/bin/sh
# Deterministic agent executable for Git resource E2E. It receives exactly the
# Manager's child environment, but performs no inference and never receives PATs.
set -eu
if [ "${1:-}" = "--version" ]; then
  echo '2.1.251 (Git resource test agent)'
  exit 0
fi
check_git_proxy() {
  if test -n "${HTTPS_PROXY:-}" && git -c credential.helper= ls-remote origin HEAD >/dev/null 2>&1; then
    echo ok > .git/oma-proxy-result
  else
    echo denied > .git/oma-proxy-result
  fi
}
check_git_proxy
while :; do
  if test -f .git/oma-proxy-check; then
    rm .git/oma-proxy-check
    check_git_proxy
  fi
  sleep 0.2
done
