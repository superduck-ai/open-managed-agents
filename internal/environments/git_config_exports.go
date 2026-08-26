package environments

import (
	"fmt"
	"strconv"
)

type gitConfigEntry struct {
	key   string
	value string
}

const environmentManagerBuiltInGitConfigCount = 3

func gitHTTPSInsteadOfEntries(host string) []gitConfigEntry {
	httpsBase := "https://" + host + "/"
	return []gitConfigEntry{
		{key: "url." + httpsBase + ".insteadOf", value: "git@" + host + ":"},
		{key: "url." + httpsBase + ".insteadOf", value: "ssh://git@" + host + "/"},
	}
}

// configuredGitSSHtoHTTPSEntries expands environment_runner.git_ssh_to_https_hosts.
// Callers must pass Load/validate-normalized hosts (trimmed, lower-cased).
// github.com is omitted — it stays as the literal KEY_1/KEY_2 exports in
// buildEnvironmentManagerCommand.
func configuredGitSSHtoHTTPSEntries(hosts []string) []gitConfigEntry {
	seen := map[string]struct{}{"github.com": {}}
	var entries []gitConfigEntry
	for _, host := range hosts {
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		entries = append(entries, gitHTTPSInsteadOfEntries(host)...)
	}
	return entries
}

// gitConfigExportLinesFrom writes shell-quoted KEY_i/VALUE_i starting at startIndex.
func gitConfigExportLinesFrom(startIndex int, entries []gitConfigEntry) []string {
	if len(entries) == 0 {
		return nil
	}
	lines := make([]string, 0, 2*len(entries))
	for i, entry := range entries {
		n := strconv.Itoa(startIndex + i)
		lines = append(lines,
			fmt.Sprintf("export GIT_CONFIG_KEY_%s=%s", n, shellQuote(entry.key)),
			fmt.Sprintf("export GIT_CONFIG_VALUE_%s=%s", n, shellQuote(entry.value)),
		)
	}
	return lines
}
