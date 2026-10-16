package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// prepareGitAwareUpload stages only Git-visible files when the source is in a
// repository. A source outside a Git work tree or a symlink source bypasses
// filtering. Git errors and empty selections fail closed. Staging keeps the
// caller's tree untouched.
func prepareGitAwareUpload(ctx context.Context, source string, enabled bool) (path string, cleanup func(), filtered bool, err error) {
	cleanup = func() {}
	if !enabled {
		return source, cleanup, false, nil
	}
	info, err := os.Lstat(source)
	if err != nil {
		return "", cleanup, false, fmt.Errorf("local upload path %q: %w", source, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return source, cleanup, false, nil
	}
	absolute, err := filepath.Abs(source)
	if err != nil {
		return "", cleanup, false, fmt.Errorf("resolve local upload path %q: %w", source, err)
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", cleanup, false, fmt.Errorf("resolve local upload path %q: %w", source, err)
	}
	gitDir := absolute
	if info.Mode().IsRegular() {
		gitDir = filepath.Dir(absolute)
	}
	rootOutput, err := runGit(ctx, gitDir, "rev-parse", "--show-toplevel")
	if err != nil {
		if isOutsideGitWorkTree(err) && !hasGitMetadata(gitDir) {
			return source, cleanup, false, nil
		}
		return "", cleanup, false, fmt.Errorf("determine Git work tree for upload %q: %w", source, err)
	}
	repoRoot, err := filepath.EvalSymlinks(strings.TrimSpace(string(rootOutput)))
	if err != nil || repoRoot == "" {
		if err == nil {
			err = fmt.Errorf("git returned an empty work tree")
		}
		return "", cleanup, false, fmt.Errorf("resolve Git work tree for upload %q: %w", source, err)
	}
	relative, err := filepath.Rel(repoRoot, absolute)
	if err != nil || !filepath.IsLocal(relative) && relative != "." {
		if err == nil {
			err = fmt.Errorf("source is outside the Git work tree")
		}
		return "", cleanup, false, fmt.Errorf("resolve Git upload path %q: %w", source, err)
	}
	args := []string{"ls-files", "-co", "--exclude-standard", "-z", "--"}
	if relative != "." {
		args = append(args, ":(literal)"+filepath.ToSlash(relative))
	}
	output, err := runGit(ctx, repoRoot, args...)
	if err != nil {
		return "", cleanup, false, fmt.Errorf("select Git-visible files for upload %q: %w", source, err)
	}
	files := bytes.Split(output, []byte{0})
	paths := make([]string, 0, len(files))
	for _, item := range files {
		if len(item) != 0 {
			paths = append(paths, string(item))
		}
	}
	if len(paths) == 0 {
		return "", cleanup, false, fmt.Errorf("git filtering selected no files from upload path %q; pass --no-git-ignore to upload unfiltered", source)
	}
	isFile := info.Mode().IsRegular()
	stageRoot, err := os.MkdirTemp("", "cautem-upload-")
	if err != nil {
		return "", cleanup, false, fmt.Errorf("stage Git-aware upload: %w", err)
	}
	rootName := filepath.Base(absolute)
	if rootName == "." || rootName == string(filepath.Separator) || rootName == "" {
		rootName = "upload"
	}
	staged := filepath.Join(stageRoot, rootName)
	cleanup = func() { _ = os.RemoveAll(stageRoot) }
	if isFile {
		if err := copyUploadEntry(filepath.Join(repoRoot, filepath.FromSlash(paths[0])), staged); err != nil {
			cleanup()
			return "", func() {}, false, fmt.Errorf("stage Git-aware upload: %w", err)
		}
		return staged, cleanup, true, nil
	}
	if err := os.Mkdir(staged, info.Mode().Perm()); err != nil {
		cleanup()
		return "", func() {}, false, fmt.Errorf("stage Git-aware upload: %w", err)
	}
	directoryModes := map[string]fs.FileMode{staged: info.Mode().Perm()}
	for _, repoPath := range paths {
		relativeFile := filepath.FromSlash(repoPath)
		if relative != "." {
			relativeFile, err = filepath.Rel(filepath.FromSlash(relative), relativeFile)
			if err != nil || !filepath.IsLocal(relativeFile) {
				cleanup()
				return "", func() {}, false, fmt.Errorf("git returned a path outside the upload root")
			}
		}
		destination := filepath.Join(staged, relativeFile)
		for parent := filepath.Dir(relativeFile); parent != "."; parent = filepath.Dir(parent) {
			sourceDir := filepath.Join(repoRoot, filepath.FromSlash(relative), parent)
			if relative == "." {
				sourceDir = filepath.Join(repoRoot, parent)
			}
			destinationDir := filepath.Join(staged, parent)
			_, ok := directoryModes[destinationDir]
			if !ok {
				dirInfo, err := os.Lstat(sourceDir)
				if err != nil {
					cleanup()
					return "", func() {}, false, fmt.Errorf("stage Git-aware upload: %w", err)
				}
				if !dirInfo.IsDir() {
					cleanup()
					return "", func() {}, false, fmt.Errorf("git upload parent %q is not a directory", sourceDir)
				}
				if err := os.MkdirAll(destinationDir, 0o700); err != nil {
					cleanup()
					return "", func() {}, false, fmt.Errorf("stage Git-aware upload: %w", err)
				}
				directoryModes[destinationDir] = dirInfo.Mode().Perm()
			}
		}
		if err := copyUploadEntry(filepath.Join(repoRoot, filepath.FromSlash(repoPath)), destination); err != nil {
			cleanup()
			return "", func() {}, false, fmt.Errorf("stage Git-aware upload: %w", err)
		}
	}
	directories := make([]string, 0, len(directoryModes))
	for directory := range directoryModes {
		directories = append(directories, directory)
	}
	sort.Slice(directories, func(i, j int) bool {
		return strings.Count(directories[i], string(filepath.Separator)) > strings.Count(directories[j], string(filepath.Separator))
	})
	for _, directory := range directories {
		if err := os.Chmod(directory, directoryModes[directory]); err != nil {
			cleanup()
			return "", func() {}, false, fmt.Errorf("stage Git-aware upload: %w", err)
		}
	}
	return staged, cleanup, true, nil
}

func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = scrubGitEnvironment(os.Environ())
	cmd.Env = append(cmd.Env, "LC_ALL=C")
	return cmd.Output()
}

func isOutsideGitWorkTree(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && bytes.Contains(exitErr.Stderr, []byte("not a git repository"))
}

func hasGitMetadata(dir string) bool {
	for current := filepath.Clean(dir); ; current = filepath.Dir(current) {
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
	}
}

func scrubGitEnvironment(environment []string) []string {
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GIT_") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func copyUploadEntry(source, destination string) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(source)
		if err != nil {
			return err
		}
		return os.Symlink(target, destination)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported Git upload entry type %s", info.Mode().Type())
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Chmod(destination, info.Mode().Perm())
}
