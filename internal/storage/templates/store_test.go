package templates

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestTemplateSaveRoundTripWorkloadResourcesAndProtectsFiles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	want := Template{
		Name: "gpu-kata", Image: "registry.example.com/team/agent:1.0", CPU: 0.5, Memory: "4Gi",
		GPUCount: 2, DriverConfigJSON: `{"docker":{"cdi_devices":["nvidia.com/gpu=0","nvidia.com/gpu=1"]}}`,
		Env: map[string]string{"FEATURE_FLAG": "on"}, Labels: map[string]string{"team": "runtime"},
	}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := Get(want.Name)
	if err != nil {
		t.Fatal(err)
	}
	if got.CPU != want.CPU || got.Memory != want.Memory || got.GPUCount != want.GPUCount || got.DriverConfigJSON != want.DriverConfigJSON || got.Env["FEATURE_FLAG"] != "on" || got.Labels["team"] != "runtime" {
		t.Fatalf("template round trip = %+v", got)
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(filepath.Join(root, "cautem", "templates")); err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("template directory mode=%v err=%v", info.Mode().Perm(), err)
		}
		if info, err := os.Stat(filepath.Join(root, "cautem", "templates", "gpu-kata.json")); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("template file mode=%v err=%v", info.Mode().Perm(), err)
		}
	}
}

func TestTemplateSaveRejectsUnsafeNamesAndConflictingImages(t *testing.T) {
	for _, name := range []string{"../escape", "../a", "Upper", "a/b", "-bad"} {
		if err := Save(Template{Name: name}); err == nil {
			t.Errorf("unsafe template name %q was accepted", name)
		}
	}
	if err := Save(Template{Name: "agent", Image: "image:tag", From: "base"}); err == nil {
		t.Fatal("template image/from conflict was accepted")
	}
}
