// Package sshconfig manages the OpenShell-style SSH client configuration:
// a cautem-owned file (~/.config/cautem/ssh_config) holding one Host
// block per sandbox, pulled into ~/.ssh/config through a single Include line.
package sshconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cautem/quarshell-cli/internal/securefile"
)

// AliasPrefix names managed hosts; legacy entries use openshell-<name>.
const AliasPrefix = "cautem-"

// Alias returns the managed Host alias for a sandbox.
func Alias(sandbox string) string { return AliasPrefix + sandbox }

// Options returns the client options every managed host uses, in order.
// Host keys are ephemeral and the transport is authenticated by the gateway
// session token, so known_hosts is disabled like OpenShell; agent and X11
// forwarding are off so host credentials never enter the sandbox.
func Options() [][2]string {
	return [][2]string{
		{"User", "sandbox"},
		{"StrictHostKeyChecking", "no"},
		{"UserKnownHostsFile", "/dev/null"},
		{"GlobalKnownHostsFile", "/dev/null"},
		{"LogLevel", "ERROR"},
		{"ServerAliveInterval", "15"},
		{"ServerAliveCountMax", "3"},
		{"ForwardAgent", "no"},
		{"ForwardX11", "no"},
	}
}

// ProxyCommand renders the ProxyCommand value: exe ssh-proxy <args...>.
func ProxyCommand(exe string, args ...string) string {
	parts := []string{Quote(exe), "ssh-proxy"}
	for _, a := range args {
		parts = append(parts, Quote(a))
	}
	return strings.Join(parts, " ")
}

// Quote double-quotes a token only when needed (works for POSIX sh and the
// Windows OpenSSH ProxyCommand parser).
func Quote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'\\$`;&|<>()*?![]{}#~") {
		return s
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// RenderHostBlock renders a managed Host block.
func RenderHostBlock(alias, proxyCommand string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Host %s\n", alias)
	for _, kv := range Options() {
		fmt.Fprintf(&b, "    %s %s\n", kv[0], kv[1])
	}
	fmt.Fprintf(&b, "    ProxyCommand %s\n", proxyCommand)
	return b.String()
}

func isBlockStart(line string) bool {
	f := strings.Fields(line)
	if len(f) == 0 {
		return false
	}
	k := strings.ToLower(f[0])
	return k == "host" || k == "match"
}

func isHostLineFor(line, alias string) bool {
	f := strings.Fields(line)
	if len(f) < 2 || strings.ToLower(f[0]) != "host" {
		return false
	}
	return slices.Contains(f[1:], alias)
}

// UpsertHostBlock replaces the Host block for alias in content (or appends it).
func UpsertHostBlock(content, alias, block string) string {
	lines := strings.Split(content, "\n")
	start, end := -1, len(lines)
	for i, l := range lines {
		if start < 0 {
			if isHostLineFor(l, alias) {
				start = i
			}
			continue
		}
		if isBlockStart(l) {
			end = i
			break
		}
	}
	block = strings.TrimRight(block, "\n")
	if start < 0 {
		trimmed := strings.TrimRight(content, "\n")
		if trimmed == "" {
			return block + "\n"
		}
		return trimmed + "\n\n" + block + "\n"
	}
	for end > start+1 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	out := append([]string{}, lines[:start]...)
	out = append(out, strings.Split(block, "\n")...)
	out = append(out, lines[end:]...)
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
}

// RemoveHostBlock deletes the Host block for alias.
func RemoveHostBlock(content, alias string) string {
	lines := strings.Split(content, "\n")
	var out []string
	skipping := false
	for _, l := range lines {
		if isHostLineFor(l, alias) {
			skipping = true
			continue
		}
		if skipping && isBlockStart(l) {
			skipping = false
		}
		if !skipping {
			out = append(out, l)
		}
	}
	s := strings.TrimRight(strings.Join(out, "\n"), "\n")
	if s == "" {
		return ""
	}
	return s + "\n"
}

// IncludeLine is the directive added to ~/.ssh/config.
func IncludeLine(managedPath string) string {
	return "Include " + Quote(managedPath)
}

// unquote reverses Quote for a double-quoted ssh_config argument.
func unquote(s string) string {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return s
	}
	return strings.NewReplacer(`\`, `\`, `"`, `"`).Replace(s[1 : len(s)-1])
}

// EnsureInclude inserts the Include line before the first Host/Match block
// (an Include inside a Host block would be scoped to that host). No-op when
// an Include for managedPath already exists.
func EnsureInclude(content, managedPath string) string {
	for l := range strings.SplitSeq(content, "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && strings.EqualFold(f[0], "include") {
			rest := strings.TrimSpace(strings.TrimSpace(l)[len(f[0]):])
			if rest == Quote(managedPath) || unquote(rest) == managedPath {
				return content
			}
			for _, p := range f[1:] {
				if strings.Trim(p, `"`) == managedPath {
					return content
				}
			}
		}
	}
	line := IncludeLine(managedPath)
	lines := strings.Split(content, "\n")
	for i, l := range lines {
		if isBlockStart(l) {
			out := append([]string{}, lines[:i]...)
			out = append(out, line, "")
			out = append(out, lines[i:]...)
			return strings.Join(out, "\n")
		}
	}
	trimmed := strings.TrimRight(content, "\n")
	if trimmed == "" {
		return line + "\n"
	}
	return trimmed + "\n" + line + "\n"
}

// Paths locates the managed file and the user's ssh config.
type Paths struct {
	Managed string
	User    string
}

// DefaultPaths returns $XDG_CONFIG_HOME/cautem/ssh_config (or
// ~/.config/cautem/ssh_config) and ~/.ssh/config.
func DefaultPaths() (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}
	cfgDir := os.Getenv("XDG_CONFIG_HOME")
	if cfgDir == "" {
		cfgDir = filepath.Join(home, ".config")
	}
	return Paths{
		Managed: filepath.Join(cfgDir, "cautem", "ssh_config"),
		User:    filepath.Join(home, ".ssh", "config"),
	}, nil
}

func readOptional(path string) (string, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(b), err
}

func writePrivate(path, content string) error {
	return securefile.WriteAtomic(path, []byte(content))
}

// Install upserts block for alias into the managed file and makes sure the
// user's ssh config includes it.
func Install(p Paths, alias, block string) error {
	cur, err := readOptional(p.Managed)
	if err != nil {
		return err
	}
	if err := writePrivate(p.Managed, UpsertHostBlock(cur, alias, block)); err != nil {
		return err
	}
	user, err := readOptional(p.User)
	if err != nil {
		return err
	}
	if next := EnsureInclude(user, p.Managed); next != user {
		return writePrivate(p.User, next)
	}
	return nil
}

// Uninstall removes alias from the managed file (the Include stays).
func Uninstall(p Paths, alias string) error {
	cur, err := readOptional(p.Managed)
	if err != nil || cur == "" {
		return err
	}
	return writePrivate(p.Managed, RemoveHostBlock(cur, alias))
}
