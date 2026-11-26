package environments

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	pipNameSeparators = regexp.MustCompile(`[-_.]+`)
	pipNameEnd        = regexp.MustCompile(`[\s\[<>=!~;@(]`)
)

func (p *environmentPackages) rejectDuplicateNames() error {
	for _, entry := range p.specsByManager() {
		seen := make(map[string]struct{}, len(*entry.specs))
		for _, spec := range *entry.specs {
			name := packageSpecName(entry.manager, spec)
			if _, found := seen[name]; found {
				return fmt.Errorf("config.packages.%s must not list package %q more than once", entry.manager, name)
			}
			seen[name] = struct{}{}
		}
	}
	return nil
}

func packageSpecName(manager, spec string) string {
	spec = strings.TrimSpace(spec)
	if manager == "pip" {
		return pipPackageName(spec)
	}
	if strings.Contains(spec, "://") {
		return spec
	}
	switch manager {
	case "apt":
		return cutAtAny(spec, "=/")
	case "gem":
		return cutAtAny(spec, ":")
	case "cargo", "go":
		return cutAtAny(spec, "@")
	case "npm":
		return npmPackageName(spec)
	}
	return spec
}

func cutAtAny(spec, separators string) string {
	if index := strings.IndexAny(spec, separators); index > 0 {
		return spec[:index]
	}
	return spec
}

func npmPackageName(spec string) string {
	if strings.HasPrefix(spec, "@") {
		if index := strings.Index(spec[1:], "@"); index >= 0 {
			return spec[:index+1]
		}
		return spec
	}
	return cutAtAny(spec, "@")
}

func pipPackageName(spec string) string {
	if location := pipNameEnd.FindStringIndex(spec); location != nil && location[0] > 0 {
		spec = spec[:location[0]]
	}
	return pipNameSeparators.ReplaceAllString(strings.ToLower(spec), "-")
}
