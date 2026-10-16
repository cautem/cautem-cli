package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareGitAwareUploadFiltersIgnoredFilesAndPreservesLinks(t *testing.T) {
	repo := t.TempDir()
	gitCommand(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("ignored.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "included.txt"), []byte("included"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "ignored.txt"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(repo, "nested")
	if err := os.Mkdir(nested, 0o751); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "mode.txt"), []byte("mode"), 0o641); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(nested, "mode.txt"), 0o641); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("included.txt", filepath.Join(repo, "link.txt")); err != nil {
		t.Fatal(err)
	}
	foreignRepo := t.TempDir()
	gitCommand(t, foreignRepo, "init", "--bare", "-q")
	t.Setenv("GIT_DIR", foreignRepo)
	staged, cleanup, filtered, err := prepareGitAwareUpload(context.Background(), repo, true)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if !filtered || staged == repo {
		t.Fatalf("plan path=%q filtered=%v; want staged Git-aware upload", staged, filtered)
	}
	root := staged
	if got, err := os.ReadFile(filepath.Join(root, "included.txt")); err != nil || string(got) != "included" {
		t.Fatalf("included entry=%q err=%v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(root, "ignored.txt")); !os.IsNotExist(err) {
		t.Fatalf("ignored entry exists or inspection failed: %v", err)
	}
	linkInfo, err := os.Lstat(filepath.Join(root, "link.txt"))
	if err != nil || linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("staged symlink info=%v err=%v", linkInfo, err)
	}
	if target, err := os.Readlink(filepath.Join(root, "link.txt")); err != nil || target != "included.txt" {
		t.Fatalf("staged symlink target=%q err=%v", target, err)
	}
	if info, err := os.Stat(filepath.Join(root, "nested")); err != nil {
		t.Fatalf("stat staged directory: %v", err)
	} else if info.Mode().Perm() != 0o751 {
		t.Fatalf("staged directory mode=%v; want 0751", info.Mode().Perm())
	}
	if info, err := os.Stat(filepath.Join(root, "nested", "mode.txt")); err != nil {
		t.Fatalf("stat staged file: %v", err)
	} else if info.Mode().Perm() != 0o641 {
		t.Fatalf("staged file mode=%v; want 0641", info.Mode().Perm())
	}
}

func TestPrepareGitAwareUploadFallsBackOnlyForNonRepo(t *testing.T) {
	nonRepo := t.TempDir()
	file := filepath.Join(nonRepo, "file.txt")
	if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, cleanup, filtered, err := prepareGitAwareUpload(context.Background(), file, true)
	cleanup()
	if err != nil || path != file || filtered {
		t.Fatalf("non-repository plan=(%q,%v,%v); want original path", path, filtered, err)
	}
	if err := os.Symlink("missing-target", filepath.Join(nonRepo, "dangling")); err != nil {
		t.Fatal(err)
	}
	path, cleanup, filtered, err = prepareGitAwareUpload(context.Background(), filepath.Join(nonRepo, "dangling"), true)
	cleanup()
	if err != nil || filtered || filepath.Base(path) != "dangling" {
		t.Fatalf("symlink plan=(%q,%v,%v); want original link path", path, filtered, err)
	}

	repo := t.TempDir()
	gitCommand(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("subdir/*.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Join(repo, "subdir")
	if err := os.Mkdir(subdir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subdir, "only.txt"), []byte("fallback"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, cleanup, filtered, err = prepareGitAwareUpload(context.Background(), subdir, true)
	cleanup()
	if err == nil || path != "" || filtered || !strings.Contains(err.Error(), "selected no files") {
		t.Fatalf("empty-selection plan=(%q,%v,%v); want fail-closed error", path, filtered, err)
	}
}

func TestPrepareGitAwareUploadFailsForBrokenRepositoryAndMissingGit(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	path, cleanup, filtered, err := prepareGitAwareUpload(context.Background(), repo, true)
	cleanup()
	if err == nil || path != "" || filtered {
		t.Fatalf("broken repository plan=(%q,%v,%v); want error", path, filtered, err)
	}

	validRepo := t.TempDir()
	gitCommand(t, validRepo, "init", "-q")
	file := filepath.Join(validRepo, "file.txt")
	if err := os.WriteFile(file, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	path, cleanup, filtered, err = prepareGitAwareUpload(context.Background(), file, true)
	cleanup()
	if err == nil || path != "" || filtered || !strings.Contains(err.Error(), "determine Git work tree") {
		t.Fatalf("missing Git plan=(%q,%v,%v); want error", path, filtered, err)
	}
}

func TestPrepareGitAwareUploadCanDisableFiltering(t *testing.T) {
	repo := t.TempDir()
	gitCommand(t, repo, "init", "-q")
	path, cleanup, filtered, err := prepareGitAwareUpload(context.Background(), repo, false)
	cleanup()
	if err != nil || path != repo || filtered {
		t.Fatalf("disabled filter plan=(%q,%v,%v); want original path", path, filtered, err)
	}
}

func gitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
