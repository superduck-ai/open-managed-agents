package networkpolicy

import "strings"

// packageManagerHosts 是受信任 package registry 与镜像 host 的有序并集。
// 镜像条目对应 Sandbox 镜像的国内镜像基线
// （见 docs/design/be/ccrv2/upstream-proxy-and-model-runtime.md 与
// docs/research/2026-07-17-package-manager-domestic-mirrors.md）；
// catalog 不包含 github.com 等 VCS host（Go `,direct` miss 在 limited 下
// 显式失败，见设计文档）。
var packageManagerHosts = []string{
	"archive.ubuntu.com",
	"security.ubuntu.com",
	"mirrors.tuna.tsinghua.edu.cn",
	"crates.io",
	"index.crates.io",
	"static.crates.io",
	"mirrors.tuna.tsinghua.edu.cn",
	"rubygems.org",
	"mirrors.tuna.tsinghua.edu.cn",
	"gems.ruby-china.com",
	"proxy.golang.org",
	"sum.golang.org",
	"goproxy.cn",
	"registry.npmjs.org",
	"registry.npmmirror.com",
	"pypi.org",
	"files.pythonhosted.org",
	"mirrors.tuna.tsinghua.edu.cn",
}

// PackageManagerHosts 返回 catalog 的去重扁平列表，顺序稳定。
func PackageManagerHosts() []string {
	return DedupeStrings(packageManagerHosts)
}

// DedupeStrings 去重并保持首次出现顺序；忽略空白与空字符串条目。
// networkpolicy 内部的 catalog/MCP hosts 与 e2bruntime 的 E2B 网络映射
// 共用本函数，避免同一语义的多份拷贝漂移。
func DedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
