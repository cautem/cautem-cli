package service

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cautem/quarshell-core/defaults"
)

// InstallOpts for `cautem install`.
type InstallOpts struct {
	Force bool
}

// Install copies this binary into ~/.local/share/cautem/bin/cautem and symlinks
// ~/.local/bin/cautem so the CLI is available from any directory.
func (a *App) Install(opt InstallOpts) error {
	src, err := os.Executable()
	if err != nil {
		return fmt.Errorf("install: resolve executable: %w", err)
	}
	src, err = filepath.EvalSymlinks(src)
	if err != nil {
		return fmt.Errorf("install: resolve executable: %w", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("install: home: %w", err)
	}
	shareBin := filepath.Join(home, ".local", "share", "cautem", "bin")
	dst := filepath.Join(shareBin, "cautem")
	linkDir := filepath.Join(home, ".local", "bin")
	link := filepath.Join(linkDir, "cautem")

	if err := os.MkdirAll(shareBin, 0o755); err != nil {
		return fmt.Errorf("install: mkdir %s: %w", shareBin, err)
	}
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		return fmt.Errorf("install: mkdir %s: %w", linkDir, err)
	}

	if err := copyFileAtomic(src, dst); err != nil {
		return fmt.Errorf("install: copy binary: %w", err)
	}
	if err := os.Chmod(dst, 0o755); err != nil {
		return fmt.Errorf("install: chmod: %w", err)
	}

	if err := ensureSymlink(link, dst, opt.Force); err != nil {
		return err
	}

	fmt.Printf("install: binary %s\n", dst)
	fmt.Printf("install: symlink %s -> %s\n", link, dst)
	if !pathContains(os.Getenv("PATH"), linkDir) {
		fmt.Printf("install: add %s to your PATH, e.g.:\n", linkDir)
		fmt.Printf("  export PATH=\"%s:$PATH\"\n", linkDir)
	} else {
		fmt.Println("install: ok — run `cautem version` from any directory")
	}
	if err := a.GatewayEnsure(); err != nil {
		fmt.Fprintf(os.Stderr, "install: gateway ensure: %v\n", err)
		fmt.Fprintf(os.Stderr, "install: start manually: cautem-gateway --listen %s\n", defaults.GatewayListen)
		return nil
	}
	return nil
}

func copyFileAtomic(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	tmp, err := os.CreateTemp(filepath.Dir(dst), "cautem-install-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}
	return os.Rename(tmpName, dst)
}

func ensureSymlink(link, target string, force bool) error {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("install: abs target: %w", err)
	}

	fi, err := os.Lstat(link)
	if err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			cur, readErr := os.Readlink(link)
			if readErr == nil {
				curAbs := cur
				if !filepath.IsAbs(cur) {
					curAbs = filepath.Join(filepath.Dir(link), cur)
				}
				if samePath(curAbs, absTarget) {
					return nil
				}
			}
		}
		if !force {
			return fmt.Errorf("install: %s already exists (use --force to replace)", link)
		}
		if err := os.Remove(link); err != nil {
			return fmt.Errorf("install: remove %s: %w", link, err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("install: lstat %s: %w", link, err)
	}

	if err := os.Symlink(absTarget, link); err != nil {
		return fmt.Errorf("install: symlink: %w", err)
	}
	return nil
}

func samePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if a == b {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA == nil && errB == nil {
		return ra == rb
	}
	return false
}

func pathContains(pathEnv, dir string) bool {
	dir = filepath.Clean(dir)
	for p := range strings.SplitSeq(pathEnv, string(os.PathListSeparator)) {
		if p == "" {
			continue
		}
		if filepath.Clean(p) == dir {
			return true
		}
	}
	return false
}
