package service

import (
	"strings"
	"testing"

	"github.com/cautem/quarshell-driver/driver"
)

func TestValidateDoctorEnginePodmanProductionPrerequisites(t *testing.T) {
	valid := driver.Probe{OK: true, ServerVersion: "6.2.1", Capabilities: []string{"libpod-native", "userns"}, Rootless: "yes"}
	if err := validateDoctorEngine("podman", valid); err != nil {
		t.Fatal(err)
	}
	old := valid
	old.ServerVersion = "5.8.7"
	if err := validateDoctorEngine("podman", old); err == nil || !strings.Contains(err.Error(), "upgrade") {
		t.Fatalf("Podman 5 error=%v", err)
	}
	missingCapability := valid
	missingCapability.Capabilities = []string{"userns"}
	if err := validateDoctorEngine("podman", missingCapability); err == nil || !strings.Contains(err.Error(), "Libpod") {
		t.Fatalf("missing capability error=%v", err)
	}
	unknownRootless := valid
	unknownRootless.Rootless = ""
	if err := validateDoctorEngine("podman", unknownRootless); err == nil || !strings.Contains(err.Error(), "rootless") {
		t.Fatalf("unknown rootless error=%v", err)
	}
}

func TestValidateDoctorEngineDockerAndVersionParsing(t *testing.T) {
	if err := validateDoctorEngine("docker", driver.Probe{OK: true}); err != nil {
		t.Fatal(err)
	}
	if major, ok := leadingVersionNumber("podman version 6.0.2"); !ok || major != 6 {
		t.Fatalf("version parse major=%d ok=%v", major, ok)
	}
	if _, ok := leadingVersionNumber("unknown"); ok {
		t.Fatal("version parser accepted an unknown version")
	}
	if err := validateDoctorEngine("docker", driver.Probe{Error: "socket refused"}); err == nil || !strings.Contains(err.Error(), "socket refused") {
		t.Fatalf("unavailable engine error=%v", err)
	}
}
