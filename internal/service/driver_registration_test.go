package service

import (
	"testing"

	"github.com/cautem/quarshell-driver/driver"
)

func TestCLIRegistersBuiltInComputeDrivers(t *testing.T) {
	if _, err := driver.OpenEngine("docker"); err != nil {
		t.Fatalf("open built-in Docker engine: %v", err)
	}
	if _, err := driver.Open("kubernetes"); err != nil {
		t.Fatalf("open built-in Kubernetes driver: %v", err)
	}
}
