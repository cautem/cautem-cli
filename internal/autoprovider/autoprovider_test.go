package autoprovider_test

import (
	"testing"

	"github.com/cautem/quarshell-cli/internal/autoprovider"
)

func TestMergeOffIgnoresInferred(t *testing.T) {
	got := autoprovider.Merge([]string{"gh"}, []string{"cursor"}, autoprovider.ModeOff)
	if len(got) != 1 || got[0] != "gh" {
		t.Fatalf("%v", got)
	}
}

func TestMergeOnUnions(t *testing.T) {
	got := autoprovider.Merge([]string{"gh"}, []string{"cursor", "gh"}, autoprovider.ModeOn)
	if len(got) != 2 || got[0] != "gh" || got[1] != "cursor" {
		t.Fatalf("%v", got)
	}
}
