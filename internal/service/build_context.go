package service

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/moby/patternmatcher"
	"github.com/moby/patternmatcher/ignorefile"
)

type buildContextArchive struct {
	reader *io.PipeReader
	done   chan error
}

func openBuildContext(ctx context.Context, root, dockerfile string) (*buildContextArchive, string, error) {
	root, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return nil, "", fmt.Errorf("resolve build context: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, "", fmt.Errorf("resolve build context: %w", err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil || !rootInfo.IsDir() {
		if err == nil {
			err = fmt.Errorf("path is not a directory")
		}
		return nil, "", fmt.Errorf("build context %q: %w", root, err)
	}
	if strings.TrimSpace(dockerfile) == "" {
		dockerfile = "Dockerfile"
	}
	if filepath.IsAbs(dockerfile) {
		return nil, "", fmt.Errorf("dockerfile path must be relative to the build context")
	}
	dockerfile = filepath.Clean(dockerfile)
	if dockerfile == "." || dockerfile == ".." || strings.HasPrefix(dockerfile, ".."+string(filepath.Separator)) {
		return nil, "", fmt.Errorf("dockerfile path must stay inside the build context")
	}
	dockerfile = filepath.ToSlash(dockerfile)
	dockerfilePath := filepath.Join(root, filepath.FromSlash(dockerfile))
	resolvedDockerfile, err := filepath.EvalSymlinks(dockerfilePath)
	if err != nil {
		return nil, "", fmt.Errorf("dockerfile %q: %w", dockerfile, err)
	}
	if !pathWithin(root, resolvedDockerfile) {
		return nil, "", fmt.Errorf("dockerfile path must stay inside the build context")
	}
	if info, err := os.Stat(resolvedDockerfile); err != nil || !info.Mode().IsRegular() {
		if err == nil {
			err = fmt.Errorf("must be a regular file")
		}
		return nil, "", fmt.Errorf("dockerfile %q: %w", dockerfile, err)
	}
	patterns, err := readDockerIgnore(filepath.Join(root, ".dockerignore"))
	if err != nil {
		return nil, "", err
	}
	matcher, err := patternmatcher.New(patterns)
	if err != nil {
		return nil, "", fmt.Errorf("parse .dockerignore: %w", err)
	}
	reader, writer := io.Pipe()
	archive := &buildContextArchive{reader: reader, done: make(chan error, 1)}
	go func() {
		err := writeBuildContextTar(ctx, writer, root, dockerfile, matcher)
		_ = writer.CloseWithError(err)
		archive.done <- err
	}()
	return archive, dockerfile, nil
}

func (a *buildContextArchive) CloseAndWait() error {
	_ = a.reader.Close()
	return <-a.done
}

func readDockerIgnore(file string) ([]string, error) {
	f, err := os.Open(file)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open .dockerignore: %w", err)
	}
	defer f.Close()
	patterns, err := ignorefile.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("read .dockerignore: %w", err)
	}
	return patterns, nil
}

func writeBuildContextTar(ctx context.Context, output io.Writer, root, dockerfile string, matcher *patternmatcher.PatternMatcher) error {
	tarWriter := tar.NewWriter(output)
	err := filepath.WalkDir(root, func(file string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := filepath.ToSlash(rel)
		if strings.Contains(name, "\\") || strings.HasPrefix(name, "../") {
			return fmt.Errorf("build context contains an unsafe path %q", name)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(file)
			if err != nil {
				return err
			}
			if filepath.IsAbs(link) || !pathWithin(root, filepath.Clean(filepath.Join(filepath.Dir(file), link))) {
				return fmt.Errorf("build context symlink %q escapes the context", name)
			}
		}
		ignored, err := matcher.MatchesOrParentMatches(name)
		if err != nil {
			return fmt.Errorf("match .dockerignore path %q: %w", name, err)
		}
		if ignored && name != dockerfile && name != ".dockerignore" {
			return nil
		}
		if info.Mode()&os.ModeType != 0 && !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			return nil
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(file)
			if err != nil {
				return err
			}
		}
		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		header.Name = name
		header.Format = tar.FormatPAX
		if info.IsDir() {
			header.Name += "/"
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			f, err := os.Open(file)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(tarWriter, f)
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}
		return nil
	})
	return errors.Join(err, tarWriter.Close())
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
