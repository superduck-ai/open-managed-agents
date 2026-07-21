package networkpolicy

import (
	"slices"

	"github.com/superduck-ai/open-managed-agents/internal/common/collections"
)

// packageManagerHosts 是受信任 package registry 与镜像 host 的有序并集。
// 镜像条目对应 Sandbox 镜像的国内镜像基线
// （见 docs/design/be/ccrv2/upstream-proxy-and-model-runtime.md 与
// docs/research/2026-07-17-package-manager-domestic-mirrors.md）；
// catalog 不包含 github.com 等 VCS host（Go `,direct` miss 在 limited 下
// 显式失败，见设计文档）。
var packageManagerHosts = collections.UniqueTrimmedStrings([]string{
	"archive.ubuntu.com",
	"security.ubuntu.com",
	"mirrors.tuna.tsinghua.edu.cn",
	"crates.io",
	"index.crates.io",
	"static.crates.io",
	"rubygems.org",
	"gems.ruby-china.com",
	"proxy.golang.org",
	"sum.golang.org",
	"storage.googleapis.com",
	"goproxy.cn",
	"registry.npmjs.org",
	"registry.npmmirror.com",
	"pypi.org",
	"files.pythonhosted.org",
})

var packageManagerHostSet = collections.StringSet(packageManagerHosts)

// PackageManagerHosts 返回 catalog 的去重扁平列表，顺序稳定；调用方可安全修改返回值。
func PackageManagerHosts() []string {
	return slices.Clone(packageManagerHosts)
}

func isPackageManagerHost(host string) bool {
	_, ok := packageManagerHostSet[host]
	return ok
}
