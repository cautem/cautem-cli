package service

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteWorkspaceListTableIncludesLabelsAndContinuation(t *testing.T) {
	var output bytes.Buffer
	page := workspaceListOutput{
		Workspaces:    []workspaceListItem{{Name: "team-a", Status: "Active", CreatedAt: "2026-10-16T00:00:00Z", Labels: map[string]string{"owner": "agents", "env": "test"}}},
		NextPageToken: "cursor-2",
	}
	if err := writeWorkspaceList(&output, "table", page); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"NAME", "STATUS", "CREATED", "LABELS", "team-a", "env=test,owner=agents", "Next page token: cursor-2"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("table output is missing %q: %s", want, output.String())
		}
	}
}

func TestWorkspaceListTableSendsContinuationTokenToDiagnostics(t *testing.T) {
	var table, diagnostics bytes.Buffer
	page := workspaceListOutput{
		Workspaces:    []workspaceListItem{{Name: "team-a"}},
		NextPageToken: "cursor-2",
	}
	if err := writeWorkspaceListWithToken(&table, &diagnostics, "table", page); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(table.String(), "cursor-2") {
		t.Fatalf("table output contains continuation token: %s", table.String())
	}
	if diagnostics.String() != "Next page token: cursor-2\n" {
		t.Fatalf("diagnostic continuation token = %q", diagnostics.String())
	}
}

func TestWriteWorkspaceListJSONIncludesPageAndLabels(t *testing.T) {
	var output bytes.Buffer
	resourceVersion := uint64(42)
	page := workspaceListOutput{
		Workspaces: []workspaceListItem{{
			ID: "ws-123", Name: "team-a", ResourceVersion: &resourceVersion, Status: "Active",
			CreatedAt: "2026-10-16 00:00:00", Labels: map[string]string{"env": "test"},
		}},
		NextPageToken: "cursor-2",
	}
	if err := writeWorkspaceList(&output, "json", page); err != nil {
		t.Fatal(err)
	}
	var decoded workspaceListOutput
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("JSON output %q: %v", output.String(), err)
	}
	if len(decoded.Workspaces) != 1 || decoded.Workspaces[0].ID != "ws-123" || decoded.Workspaces[0].Name != "team-a" || decoded.Workspaces[0].ResourceVersion == nil || *decoded.Workspaces[0].ResourceVersion != 42 || decoded.Workspaces[0].Status != "Active" || decoded.Workspaces[0].CreatedAt != "2026-10-16 00:00:00" || decoded.Workspaces[0].Labels["env"] != "test" || decoded.NextPageToken != "cursor-2" {
		t.Fatalf("decoded workspace page = %+v", decoded)
	}
}

func TestWriteWorkspaceListJSONAlwaysIncludesEmptyContinuationToken(t *testing.T) {
	var output bytes.Buffer
	if err := writeWorkspaceList(&output, "json", workspaceListOutput{Workspaces: []workspaceListItem{}}); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("JSON output %q: %v", output.String(), err)
	}
	if token, ok := decoded["next_page_token"]; !ok || string(token) != `""` {
		t.Fatalf("next_page_token = %s, present=%t", token, ok)
	}
}

func TestWriteWorkspaceListYAMLEmitsPaginationEnvelope(t *testing.T) {
	var output bytes.Buffer
	if err := writeWorkspaceList(&output, "yaml", workspaceListOutput{
		Workspaces: []workspaceListItem{{Name: "team-a"}}, NextPageToken: "cursor-2",
	}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"workspaces:", "name: team-a", "next_page_token: cursor-2"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("YAML output is missing %q: %s", want, output.String())
		}
	}
}

func TestParseWorkspaceLabelSelectorMatchesPinnedGrammar(t *testing.T) {
	got, err := parseWorkspaceLabelSelector(" env = test, owner=agents, env=staging, ")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["env"] != "staging" || got["owner"] != "agents" {
		t.Fatalf("selector requirements = %#v", got)
	}
	if empty, err := parseWorkspaceLabelSelector(" , "); err != nil || len(empty) != 0 {
		t.Fatalf("empty selector = %#v, err %v", empty, err)
	}
	if emptyValue, err := parseWorkspaceLabelSelector("env="); err != nil || emptyValue["env"] != "" {
		t.Fatalf("empty label value = %#v, err %v", emptyValue, err)
	}
}

func TestParseWorkspaceLabelSelectorRejectsInvalidAndExcessivePairs(t *testing.T) {
	for _, selector := range []string{"env", "=test", "env=not valid", strings.Repeat("k=v,", 64) + "last=value"} {
		if _, err := parseWorkspaceLabelSelector(selector); err == nil {
			t.Errorf("parseWorkspaceLabelSelector(%q) unexpectedly succeeded", selector)
		}
	}
}

func TestWorkspaceLabelsMatchRequiresEverySelectorPair(t *testing.T) {
	labels := map[string]any{"env": "test", "owner": "agents"}
	if !workspaceLabelsMatch(labels, map[string]string{"env": "test", "owner": "agents"}) {
		t.Fatal("matching selector pairs did not match")
	}
	if workspaceLabelsMatch(labels, map[string]string{"env": "test", "owner": "someone-else"}) {
		t.Fatal("selector matched when one label value differed")
	}
}

func TestFormatWorkspaceTimestampMatchesPinnedCLIFormat(t *testing.T) {
	got := formatWorkspaceTimestamp("2026-10-16T03:04:05+03:00")
	if got != "2026-10-16 00:04:05" {
		t.Fatalf("formatted workspace timestamp = %q", got)
	}
}
