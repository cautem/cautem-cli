package service

import "strings"

// localExecArgv mirrors OpenShell's shell startup behavior for local
// Docker/Podman sandboxes. The command is serialized as POSIX shell words and
// passed as a positional argument to a small shell selector, so user arguments
// never become part of the selector script.
func localExecArgv(argv []string, noLoginShell bool) []string {
	words := make([]string, len(argv))
	for i, arg := range argv {
		words[i] = quotePOSIXShellWord(arg)
	}
	command := strings.Join(words, " ")
	shellFlag := "-lc"
	if noLoginShell {
		shellFlag = "-c"
	}
	selector := `if [ -x /bin/bash ]; then exec /bin/bash ` + shellFlag + ` "$1"; ` +
		`elif [ -x /usr/bin/bash ]; then exec /usr/bin/bash ` + shellFlag + ` "$1"; ` +
		`elif [ -x /bin/sh ]; then exec /bin/sh ` + shellFlag + ` "$1"; ` +
		`else echo 'cautem exec: no supported shell found' >&2; exit 127; fi`
	return []string{"/bin/sh", "-c", selector, "cautem-exec", command}
}

func quotePOSIXShellWord(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
