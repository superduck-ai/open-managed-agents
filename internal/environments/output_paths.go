package environments

import "path"

const (
	rcloneOutputsDestination = "/mnt/user-data/outputs"
	sessionOutputsPath       = "/mnt/session/outputs"
)

func sessionOutputsAliasCommand(target, alias string) string {
	quotedAlias := shellQuote(alias)
	quotedTarget := shellQuote(target)
	return "mkdir -p " + shellQuote(path.Dir(alias)) +
		" && if [ -L " + quotedAlias + " ]; then test \"$(readlink " + quotedAlias + ")\" = " + quotedTarget +
		"; else if [ -e " + quotedAlias + " ]; then rmdir " + quotedAlias + " || exit 1; fi; ln -s " + quotedTarget + " " + quotedAlias + "; fi"
}
