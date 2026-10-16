package service

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cautem/quarshell-sdk/go/cautem"
)

// uploadGatewayPath sends a tar archive over the Gateway's existing interactive
// exec stream. The remote script validates canonical destinations against the
// sandbox workspace before extracting any input.
func uploadGatewayPath(ctx context.Context, client gatewayExecClient, workspace, sandbox, source, destination string, timeout time.Duration) error {
	archive, kind, base, err := makeUploadArchive(source)
	if err != nil {
		return err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind upload archive: %w", err)
	}
	argv := []string{"sh", "-c", gatewayUploadScript, "cautem-upload", destination, kind, base}
	session, err := client.ExecInteractiveWithTTYTimeout(ctx, workspace, sandbox, argv, false, 0, 0, timeout, cautem.SandboxExecOptions{NoLoginShell: true})
	if err != nil {
		return fmt.Errorf("start Gateway upload to sandbox %q: %w", sandbox, err)
	}
	defer session.Close()

	copyDone := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(session, archive)
		copyDone <- errors.Join(copyErr, cautem.CloseSandboxExecInput(session))
	}()
	var outputErr error
	for {
		chunk, nextErr := session.NextOutput()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			outputErr = nextErr
			_ = session.Close()
			break
		}
		writer := io.Writer(os.Stdout)
		if chunk.Stream == "stderr" {
			writer = os.Stderr
		}
		if _, err := writer.Write(chunk.Data); err != nil {
			outputErr = fmt.Errorf("write Gateway upload output: %w", err)
			_ = session.Close()
			break
		}
	}
	inputErr := <-copyDone
	code, exitErr := session.ExitCode()
	if code != 0 && exitErr == nil {
		exitErr = fmt.Errorf("remote upload command exited with status %d", code)
	}
	return errors.Join(outputErr, inputErr, exitErr)
}

func makeUploadArchive(source string) (*os.File, string, string, error) {
	absolute, err := filepath.Abs(source)
	if err != nil {
		return nil, "", "", fmt.Errorf("resolve local upload path %q: %w", source, err)
	}
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, "", "", fmt.Errorf("local upload path %q: %w", source, err)
	}
	base := filepath.Base(absolute)
	kind := "file"
	if info.IsDir() {
		kind = "directory"
		if filepath.Clean(source) == "." || filepath.Clean(source) == string(filepath.Separator) {
			kind = "contents"
		}
	} else if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return nil, "", "", fmt.Errorf("unsupported upload entry type %s", info.Mode().Type())
	}
	archive, err := os.CreateTemp("", "cautem-transfer-*.tar")
	if err != nil {
		return nil, "", "", fmt.Errorf("create upload archive: %w", err)
	}
	removeOnError := true
	defer func() {
		if removeOnError {
			_ = archive.Close()
			_ = os.Remove(archive.Name())
		}
	}()
	w := tar.NewWriter(archive)
	add := func(path, name string, entry fs.FileInfo) error {
		header, err := tar.FileInfoHeader(entry, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(name)
		if entry.Mode()&os.ModeSymlink != 0 {
			header.Linkname, err = os.Readlink(path)
			if err != nil {
				return err
			}
			header.Linkname = filepath.ToSlash(header.Linkname)
		}
		header.Uid, header.Gid = 0, 0
		header.Uname, header.Gname = "", ""
		if err := w.WriteHeader(header); err != nil {
			return err
		}
		if !entry.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(w, f)
		return errors.Join(copyErr, f.Close())
	}
	if info.IsDir() {
		err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			entryInfo, err := os.Lstat(path)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(absolute, path)
			if err != nil {
				return err
			}
			name := filepath.Join(base, relative)
			if kind == "contents" {
				name = relative
				if relative == "." {
					return nil
				}
			}
			return add(path, name, entryInfo)
		})
	} else {
		err = add(source, base, info)
	}
	err = errors.Join(err, w.Close())
	if err != nil {
		return nil, "", "", fmt.Errorf("build upload archive: %w", err)
	}
	removeOnError = false
	return archive, kind, base, nil
}

const gatewayUploadScript = `set -eu
root=$(pwd -P)
case "$root" in /*) ;; *) echo "workspace root is not absolute" >&2; exit 1 ;; esac
[ "$root" != / ] || { echo "workspace root cannot be /" >&2; exit 1; }
dest=$1
kind=$2
base=$3
case "$dest" in /*) candidate=$dest ;; *) candidate=$root/$dest ;; esac
prefix=${root%/}/
check_workspace_path() {
  case "$1" in "$root"|"$prefix"*) ;; *) echo "upload destination escapes sandbox workspace" >&2; exit 1 ;; esac
}
if [ "$kind" = directory ] || [ "$kind" = contents ]; then
  if [ -e "$candidate" ] || [ -L "$candidate" ]; then
    [ -d "$candidate" ] || { echo "directory upload destination is not a directory" >&2; exit 1; }
    resolved=$(realpath "$candidate")
  else
    parent=$(dirname -- "$candidate")
    parent_real=$(realpath "$parent")
    check_workspace_path "$parent_real"
    resolved=$parent_real/$(basename -- "$candidate")
    mkdir -p -- "$candidate"
  fi
  resolved=$(realpath "$candidate")
  check_workspace_path "$resolved"
  tmp=$(mktemp -d "$root/.cautem-upload.XXXXXX")
  trap 'rm -rf -- "$tmp"' EXIT HUP INT TERM
  tar -xf - -C "$tmp"
  cp -a "$tmp/." "$resolved/"
  exit 0
fi
expected_kind=missing
case "$candidate" in */) is_dir=1 ;; *) is_dir=0 ;; esac
if [ -d "$candidate" ]; then
  is_dir=1
  expected_kind=directory
elif [ -e "$candidate" ] || [ -L "$candidate" ]; then
  expected_kind=file
fi
if [ "$is_dir" = 1 ]; then
  if [ ! -d "$candidate" ]; then
    parent=$(dirname -- "${candidate%/}")
    parent_real=$(realpath "$parent")
    check_workspace_path "$parent_real"
    mkdir -p -- "$candidate"
  fi
  resolved=$(realpath "$candidate")
  check_workspace_path "$resolved"
  target=$resolved/$base
else
  if [ -L "$candidate" ] && [ ! -e "$candidate" ]; then
    parent=$(dirname -- "$candidate")
    parent_real=$(realpath "$parent")
    check_workspace_path "$parent_real"
    target=$parent_real/$(basename -- "$candidate")
  elif [ "$expected_kind" = file ]; then
    target=$(realpath "$candidate")
  else
    parent=$(dirname -- "$candidate")
    parent_real=$(realpath "$parent")
    check_workspace_path "$parent_real"
    target=$parent_real/$(basename -- "$candidate")
  fi
fi
check_workspace_path "$target"
[ ! -d "$target" ] || { echo "file upload destination is a directory" >&2; exit 1; }
if [ "$is_dir" = 0 ]; then
  if [ "$expected_kind" = directory ] && [ ! -d "$candidate" ]; then
    echo "upload destination changed type before extraction" >&2; exit 1
  elif [ "$expected_kind" = file ] && { [ -d "$candidate" ] || { [ ! -e "$candidate" ] && [ ! -L "$candidate" ]; }; }; then
    echo "upload destination changed type before extraction" >&2; exit 1
  elif [ "$expected_kind" = missing ] && { [ -e "$candidate" ] || [ -L "$candidate" ]; }; then
    echo "upload destination changed type before extraction" >&2; exit 1
  fi
fi
tmp=$(mktemp -d "$root/.cautem-upload.XXXXXX")
trap 'rm -rf -- "$tmp"' EXIT HUP INT TERM
tar -xf - -C "$tmp"
mv -f -- "$tmp/$base" "$target"
`

func downloadGatewayPath(ctx context.Context, client gatewayExecClient, workspace, sandbox, source, destination string, timeout time.Duration) error {
	archive, err := captureGatewayDownloadArchive(ctx, client, workspace, sandbox, source, timeout)
	if err != nil {
		return err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind Gateway download archive: %w", err)
	}
	kind, basename, err := inspectGatewayDownloadArchive(archive)
	if err != nil {
		return err
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind Gateway download archive: %w", err)
	}
	if kind == "directory" {
		if info, statErr := os.Lstat(destination); statErr == nil && !info.IsDir() {
			return fmt.Errorf("sandbox download: cannot extract directory %q over non-directory destination %q", source, destination)
		} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("inspect local download destination %q: %w", destination, statErr)
		}
		if err := os.MkdirAll(destination, 0o755); err != nil {
			return fmt.Errorf("create local download destination %q: %w", destination, err)
		}
		if err := extractGatewayDownloadTar(archive, destination); err != nil {
			return fmt.Errorf("extract sandbox directory %q: %w", source, err)
		}
		return nil
	}

	info, statErr := os.Lstat(destination)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("inspect local download destination %q: %w", destination, statErr)
	}
	asDirectory := strings.HasSuffix(destination, "/") || (string(os.PathSeparator) != "/" && strings.HasSuffix(destination, string(os.PathSeparator)))
	if statErr == nil && info.IsDir() {
		asDirectory = true
	}
	target := destination
	if asDirectory {
		target = filepath.Join(destination, basename)
	}
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create local download parent %q: %w", parent, err)
	}
	staging, err := os.MkdirTemp(parent, ".cautem-download-*")
	if err != nil {
		return fmt.Errorf("create local download staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	summary, err := extractGatewayDownloadTarWithSummary(archive, staging)
	if err != nil {
		return fmt.Errorf("extract sandbox file %q: %w", source, err)
	}
	if summary.kind != "file" || summary.fileName != basename || summary.fileCount != 1 || summary.entryCount != 1 {
		return fmt.Errorf("sandbox download: expected one regular file %q in archive", basename)
	}
	if info, err := os.Lstat(target); err == nil && info.IsDir() {
		return fmt.Errorf("sandbox download: cannot overwrite directory %q with file %q", target, source)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect local download target %q: %w", target, err)
	}
	if err := os.Rename(filepath.Join(staging, basename), target); err != nil {
		return fmt.Errorf("place downloaded file at %q: %w", target, err)
	}
	return nil
}

func captureGatewayDownloadArchive(ctx context.Context, client gatewayExecClient, workspace, sandbox, source string, timeout time.Duration) (*os.File, error) {
	archive, err := os.CreateTemp("", "cautem-download-*.tar")
	if err != nil {
		return nil, fmt.Errorf("create Gateway download archive: %w", err)
	}
	removeOnError := true
	defer func() {
		if removeOnError {
			_ = archive.Close()
			_ = os.Remove(archive.Name())
		}
	}()
	argv := []string{"sh", "-c", gatewayDownloadScript, "cautem-download", source}
	stream, err := client.ExecStreamingWithTimeout(ctx, workspace, sandbox, argv, timeout, cautem.SandboxExecOptions{NoLoginShell: true})
	if err != nil {
		return nil, fmt.Errorf("start Gateway download from sandbox %q: %w", sandbox, err)
	}
	defer stream.Close()
	for {
		chunk, nextErr := stream.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return nil, fmt.Errorf("read Gateway download stream: %w", nextErr)
		}
		if chunk.Stream == "stderr" {
			if _, err := os.Stderr.Write(chunk.Data); err != nil {
				return nil, fmt.Errorf("write Gateway download diagnostics: %w", err)
			}
			continue
		}
		if _, err := archive.Write(chunk.Data); err != nil {
			return nil, fmt.Errorf("save Gateway download archive: %w", err)
		}
	}
	code, err := stream.ExitCode()
	if err != nil {
		return nil, fmt.Errorf("get Gateway download exit status: %w", err)
	}
	if code != 0 {
		return nil, fmt.Errorf("gateway download command exited with status %d", code)
	}
	if err := archive.Sync(); err != nil {
		return nil, fmt.Errorf("flush Gateway download archive: %w", err)
	}
	removeOnError = false
	return archive, nil
}

const legacyTarRegularType = byte(0)

type gatewayDownloadArchiveSummary struct {
	kind       string
	fileName   string
	fileCount  int
	entryCount int
}

func inspectGatewayDownloadArchive(archive *os.File) (kind, basename string, err error) {
	reader := tar.NewReader(archive)
	header, err := reader.Next()
	if err != nil {
		return "", "", fmt.Errorf("inspect Gateway download archive: %w", err)
	}
	name, err := cleanGatewayArchiveName(header.Name)
	if err != nil {
		return "", "", err
	}
	if header.Typeflag == tar.TypeDir {
		return "directory", "", nil
	}
	if header.Typeflag != tar.TypeReg && header.Typeflag != legacyTarRegularType {
		return "", "", fmt.Errorf("gateway download archive has unsupported root entry %q", header.Name)
	}
	return "file", filepath.Base(name), nil
}

func extractGatewayDownloadTar(archive *os.File, destination string) error {
	summary, err := extractGatewayDownloadTarWithSummary(archive, destination)
	if err != nil {
		return err
	}
	if summary.kind != "directory" {
		return fmt.Errorf("expected directory archive")
	}
	return nil
}

func extractGatewayDownloadTarWithSummary(archive *os.File, destination string) (gatewayDownloadArchiveSummary, error) {
	root, err := os.OpenRoot(destination)
	if err != nil {
		return gatewayDownloadArchiveSummary{}, err
	}
	defer root.Close()
	reader := tar.NewReader(archive)
	var summary gatewayDownloadArchiveSummary
	directoryModes := map[string]os.FileMode{}
	archiveRegularFiles := map[string]bool{}
	first := true
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return gatewayDownloadArchiveSummary{}, err
		}
		summary.entryCount++
		name, err := cleanGatewayArchiveName(header.Name)
		if err != nil {
			return gatewayDownloadArchiveSummary{}, err
		}
		if first {
			first = false
			switch header.Typeflag {
			case tar.TypeDir:
				summary.kind = "directory"
			case tar.TypeReg, legacyTarRegularType:
				summary.kind, summary.fileName = "file", filepath.Base(name)
			default:
				return gatewayDownloadArchiveSummary{}, fmt.Errorf("unsupported archive root entry %q", header.Name)
			}
		}
		if name == "." && header.Typeflag == tar.TypeDir {
			continue
		}
		if err := rejectGatewayArchiveSymlink(root, name); err != nil {
			return gatewayDownloadArchiveSummary{}, err
		}
		mode := os.FileMode(header.Mode) & 0o777
		switch header.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(name, 0o700); err != nil {
				return gatewayDownloadArchiveSummary{}, err
			}
			directoryModes[name] = mode
		case tar.TypeReg, legacyTarRegularType:
			if err := root.MkdirAll(filepath.Dir(name), 0o700); err != nil {
				return gatewayDownloadArchiveSummary{}, err
			}
			file, err := root.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
			if err != nil {
				return gatewayDownloadArchiveSummary{}, err
			}
			_, copyErr := io.Copy(file, reader)
			chmodErr := file.Chmod(mode)
			closeErr := file.Close()
			if err := errors.Join(copyErr, chmodErr, closeErr); err != nil {
				return gatewayDownloadArchiveSummary{}, err
			}
			archiveRegularFiles[name] = true
			summary.fileCount++
		case tar.TypeSymlink:
			if err := root.MkdirAll(filepath.Dir(name), 0o700); err != nil {
				return gatewayDownloadArchiveSummary{}, err
			}
			if err := root.Symlink(header.Linkname, name); err != nil {
				return gatewayDownloadArchiveSummary{}, err
			}
		case tar.TypeLink:
			linkName, err := cleanGatewayArchiveName(header.Linkname)
			if err != nil {
				return gatewayDownloadArchiveSummary{}, fmt.Errorf("invalid archive hard link: %w", err)
			}
			if err := rejectGatewayArchiveSymlink(root, linkName); err != nil {
				return gatewayDownloadArchiveSummary{}, err
			}
			if !archiveRegularFiles[linkName] {
				return gatewayDownloadArchiveSummary{}, fmt.Errorf("archive hard link %q does not reference a prior regular file", header.Linkname)
			}
			if err := root.MkdirAll(filepath.Dir(name), 0o700); err != nil {
				return gatewayDownloadArchiveSummary{}, err
			}
			if err := root.Link(linkName, name); err != nil {
				return gatewayDownloadArchiveSummary{}, err
			}
			archiveRegularFiles[name] = true
		default:
			return gatewayDownloadArchiveSummary{}, fmt.Errorf("unsupported archive entry type %d for %q", header.Typeflag, header.Name)
		}
	}
	if first {
		return gatewayDownloadArchiveSummary{}, fmt.Errorf("gateway download archive is empty")
	}
	directories := make([]string, 0, len(directoryModes))
	for name := range directoryModes {
		directories = append(directories, name)
	}
	sort.Slice(directories, func(i, j int) bool {
		return strings.Count(directories[i], string(os.PathSeparator)) > strings.Count(directories[j], string(os.PathSeparator))
	})
	for _, name := range directories {
		if err := root.Chmod(name, directoryModes[name]); err != nil {
			return gatewayDownloadArchiveSummary{}, err
		}
	}
	return summary, nil
}

func cleanGatewayArchiveName(raw string) (string, error) {
	if raw == "" || strings.ContainsRune(raw, '\x00') || path.IsAbs(raw) || strings.HasPrefix(raw, `\`) {
		return "", fmt.Errorf("invalid gateway download archive path %q", raw)
	}
	for _, part := range strings.Split(strings.ReplaceAll(raw, `\`, "/"), "/") {
		if part == ".." {
			return "", fmt.Errorf("gateway download archive path escapes destination: %q", raw)
		}
	}
	clean := path.Clean(raw)
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, ":") && len(strings.Split(clean, "/")[0]) == 2 {
		return "", fmt.Errorf("gateway download archive path escapes destination: %q", raw)
	}
	return filepath.FromSlash(clean), nil
}

func rejectGatewayArchiveSymlink(root *os.Root, name string) error {
	current := ""
	for _, part := range strings.Split(filepath.Clean(name), string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("gateway download archive traverses existing symlink %q", current)
		}
	}
	return nil
}

const gatewayDownloadScript = `set -eu
root=$(pwd -P)
case "$root" in /*) ;; *) echo "workspace root is not absolute" >&2; exit 1 ;; esac
[ "$root" != / ] || { echo "workspace root cannot be /" >&2; exit 1; }
source_path=$1
case "$source_path" in /*) candidate=$source_path ;; *) candidate=$root/$source_path ;; esac
[ -e "$candidate" ] || [ -L "$candidate" ] || { echo "sandbox source does not exist" >&2; exit 1; }
resolved=$(realpath "$candidate") || { echo "sandbox source does not exist" >&2; exit 1; }
prefix=${root%/}/
case "$resolved" in "$root"|"$prefix"*) ;; *) echo "sandbox source escapes workspace" >&2; exit 1 ;; esac
if [ -d "$resolved" ]; then
  parent=$(dirname -- "$resolved")
  base=$(basename -- "$resolved")
  exec tar cf - -C "$parent" -- "$base"
fi
parent=$(dirname -- "$resolved")
base=$(basename -- "$resolved")
exec tar cf - -C "$parent" -- "$base"
`
