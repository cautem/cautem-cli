package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/cautem/cautem-driver/driver"
)

func buildLocalSourceImage(ctx context.Context, compute driver.ComputeDriver, contextDir, dockerfile, sandboxName string, output io.Writer) (string, error) {
	builder, ok := compute.(driver.ImageBuilder)
	if !ok {
		return "", fmt.Errorf("compute driver %q does not support local image builds", driverName(compute))
	}
	archive, dockerfile, err := openBuildContext(ctx, contextDir, dockerfile)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(sandboxName))
	tag := "cautem-local/build:" + hex.EncodeToString(sum[:6])
	if output == nil {
		output = io.Discard
	}
	fmt.Fprintf(output, "sandbox create: building %s from %s\n", tag, contextDir)
	buildErr := builder.BuildImage(ctx, driver.ImageBuildRequest{Context: archive.reader, Dockerfile: dockerfile, Tag: tag, Output: output})
	archiveErr := archive.CloseAndWait()
	if buildErr != nil {
		return "", buildErr
	}
	if archiveErr != nil {
		return "", fmt.Errorf("archive build context: %w", archiveErr)
	}
	return tag, nil
}

func driverName(compute driver.ComputeDriver) string {
	if compute == nil {
		return "unknown"
	}
	name := fmt.Sprintf("%T", compute)
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		name = name[i+1:]
	}
	return strings.TrimPrefix(name, "*")
}
