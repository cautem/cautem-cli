package osargs_test

import (
	"testing"

	"github.com/cautem/quarshell-cli/internal/osargs"
)

func TestParseInferenceSet(t *testing.T) {
	got, err := osargs.ParseInferenceSet([]string{"--provider", "ollama", "--model", "llama3", "--timeout", "120", "--no-verify"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "ollama" || got.Model != "llama3" || got.TimeoutSec != 120 || !got.NoVerify {
		t.Fatalf("%+v", got)
	}
}
