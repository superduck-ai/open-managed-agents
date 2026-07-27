// 本文件改编自 k8s.io/apimachinery@v0.35.6 pkg/util/validation/validation.go
// （Apache License 2.0）：只 vendored allowed_hosts 校验所需的最小子集
// （IsDNS1123Subdomain 与 IsWildcardDNS1123Subdomain 及其依赖的正则、长度与
// 错误信息常量），正则与错误信息与上游逐字一致，避免为了两个函数引入
// k8s.io/apimachinery、k8s.io/klog、k8s.io/utils 整条依赖链。
package networkpolicy

import (
	"fmt"
	"regexp"
)

const dns1123LabelFmt string = "[a-z0-9]([-a-z0-9]*[a-z0-9])?"

const dns1123SubdomainFmt string = dns1123LabelFmt + "(\\." + dns1123LabelFmt + ")*"
const dns1123SubdomainErrorMsg string = "a lowercase RFC 1123 subdomain must consist of lower case alphanumeric characters, '-' or '.', and must start and end with an alphanumeric character"

// dns1123SubdomainMaxLength is a subdomain's max length in DNS (RFC 1123).
const dns1123SubdomainMaxLength int = 253

var dns1123SubdomainRegexp = regexp.MustCompile("^" + dns1123SubdomainFmt + "$")

const wildcardDNS1123SubdomainFmt = "\\*\\." + dns1123SubdomainFmt
const wildcardDNS1123SubdomainErrMsg = "a wildcard DNS-1123 subdomain must start with '*.', followed by a valid DNS subdomain, which must consist of lower case alphanumeric characters, '-' or '.' and end with an alphanumeric character"

var wildcardDNS1123SubdomainRegexp = regexp.MustCompile("^" + wildcardDNS1123SubdomainFmt + "$")

// isDNS1123Subdomain tests for a string that conforms to the definition of a
// subdomain in DNS (RFC 1123).
func isDNS1123Subdomain(value string) []string {
	var errs []string
	if len(value) > dns1123SubdomainMaxLength {
		errs = append(errs, maxLenError(dns1123SubdomainMaxLength))
	}
	if !dns1123SubdomainRegexp.MatchString(value) {
		errs = append(errs, regexError(dns1123SubdomainErrorMsg, dns1123SubdomainFmt, "example.com"))
	}
	return errs
}

// isWildcardDNS1123Subdomain tests for a string that conforms to the definition
// of a wildcard subdomain in DNS (RFC 1034 section 4.3.3).
func isWildcardDNS1123Subdomain(value string) []string {
	var errs []string
	if len(value) > dns1123SubdomainMaxLength {
		errs = append(errs, maxLenError(dns1123SubdomainMaxLength))
	}
	if !wildcardDNS1123SubdomainRegexp.MatchString(value) {
		errs = append(errs, regexError(wildcardDNS1123SubdomainErrMsg, wildcardDNS1123SubdomainFmt, "*.example.com"))
	}
	return errs
}

// maxLenError returns a string explanation of a "string too long" validation
// failure.
func maxLenError(length int) string {
	return fmt.Sprintf("must be no more than %d characters", length)
}

// regexError returns a string explanation of a regex validation failure.
// 参数名 pattern 对应上游的 fmt（上游以该名字遮蔽了 fmt 包，此处重命名以
// 符合本仓库命名习惯，行为不变）。
func regexError(msg string, pattern string, examples ...string) string {
	if len(examples) == 0 {
		return msg + " (regex used for validation is '" + pattern + "')"
	}
	msg += " (e.g. "
	for i := range examples {
		if i > 0 {
			msg += " or "
		}
		msg += "'" + examples[i] + "', "
	}
	msg += "regex used for validation is '" + pattern + "')"
	return msg
}
