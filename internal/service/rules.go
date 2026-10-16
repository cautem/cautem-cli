// SPDX-FileCopyrightText: Copyright (c) 2026 cautem
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cautem/quarshell-sdk/go/cautem"
)

// RuleListFilter lists policy.local proposals (gateway) with local fallback.
func (a *App) RuleListFilter(sandbox, status string) error {
	if sandbox != "" {
		if gw, err := a.currentGatewayURL(); err == nil && gw != "" {
			c := cautem.NewWithToken(gw, a.gatewayTokenForURL(gw))
			ctx, cancel := a.withTimeout(TimeoutAPI)
			defer cancel()
			list, err := c.ListProposals(ctx, sandbox, status)
			if err == nil {
				b, _ := json.MarshalIndent(list, "", "  ")
				fmt.Println(string(b))
				return nil
			}
			fmt.Fprintf(os.Stderr, "rule: gateway list: %v (falling back to local)\n", err)
		}
	} else if gw, err := a.currentGatewayURL(); err == nil && gw != "" {
		c := cautem.NewWithToken(gw, a.gatewayTokenForURL(gw))
		ctx, cancel := a.withTimeout(TimeoutAPI)
		defer cancel()
		var all []cautem.Proposal
		for _, sb := range a.ruleCandidateSandboxes(ctx, c) {
			list, err := c.ListProposals(ctx, sb, status)
			if err != nil {
				continue
			}
			all = append(all, list...)
		}
		if len(all) > 0 {
			b, _ := json.MarshalIndent(all, "", "  ")
			fmt.Println(string(b))
			return nil
		}
	}
	return a.ruleListLocal(sandbox, status)
}

func (a *App) ruleListLocal(sandbox, status string) error {
	dir, err := localParityDir()
	if err != nil {
		return err
	}
	m, err := readJSONMap(filepath.Join(dir, "rules.json"))
	if err != nil {
		return err
	}
	filtered := map[string]any{}
	for id, raw := range m {
		row, _ := raw.(map[string]any)
		if row == nil {
			continue
		}
		if sandbox != "" {
			if sb, _ := row["sandbox"].(string); sb != "" && sb != sandbox {
				continue
			}
		}
		if status != "" {
			st, _ := row["state"].(string)
			if st == "" {
				st, _ = row["status"].(string)
			}
			if st != status {
				continue
			}
		}
		filtered[id] = row
	}
	b, _ := json.MarshalIndent(filtered, "", "  ")
	fmt.Println(string(b))
	return nil
}

// RuleApprove approves a proposal, merges into sandbox base, writes live policy bind.
func (a *App) RuleApprove(id string) error {
	return a.ruleDecide(id, true, "")
}

// RuleApproveAll approves pending proposals, retaining security-flagged ones
// unless the caller explicitly opts in. It never clears proposal history.
func (a *App) RuleApproveAll(sandbox string, includeSecurityFlagged bool) error {
	gw, err := a.currentGatewayURL()
	if err != nil {
		if !errors.Is(err, errNoCurrentGateway) {
			return err
		}
		return a.ruleApproveAllLocal(sandbox, includeSecurityFlagged)
	}
	c := cautem.NewWithToken(gw, a.gatewayTokenForURL(gw))
	ctx, cancel := a.withTimeout(TimeoutAPILong)
	defer cancel()
	sandboxes := []string{sandbox}
	if sandbox == "" {
		sandboxes = a.ruleCandidateSandboxes(ctx, c)
		if len(sandboxes) == 0 {
			return fmt.Errorf("rule approve-all: no sandboxes available")
		}
	}
	approved, skipped := 0, 0
	for _, sb := range sandboxes {
		pending, err := c.ListProposals(ctx, sb, "pending")
		if err != nil {
			return fmt.Errorf("rule approve-all: list %s: %w", sb, err)
		}
		sort.Slice(pending, func(i, j int) bool { return pending[i].ID < pending[j].ID })
		for _, p := range pending {
			if p.SecurityFlagged && !includeSecurityFlagged {
				skipped++
				continue
			}
			if _, err := c.ApproveProposal(ctx, sb, p.ID); err != nil {
				return fmt.Errorf("rule approve-all: approved %d, failed %s: %w", approved, p.ID, err)
			}
			if err := a.refreshApprovedPolicy(ctx, c, sb); err != nil {
				return fmt.Errorf("rule approve-all: approved %d, live policy for %s: %w", approved+1, sb, err)
			}
			approved++
		}
	}
	fmt.Printf("rule approve-all: %d approved, %d security-flagged skipped\n", approved, skipped)
	return nil
}

func (a *App) ruleApproveAllLocal(sandbox string, includeSecurityFlagged bool) error {
	dir, err := localParityDir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "rules.json")
	m, err := readJSONMap(path)
	if err != nil {
		return err
	}
	approved, skipped := 0, 0
	for _, raw := range m {
		row, ok := raw.(map[string]any)
		if !ok || row == nil {
			continue
		}
		if sandbox != "" && row["sandbox"] != sandbox {
			continue
		}
		status, _ := row["status"].(string)
		if status == "" {
			status, _ = row["state"].(string)
		}
		if status != "pending" {
			continue
		}
		flagged, _ := row["security_flagged"].(bool)
		if flagged && !includeSecurityFlagged {
			skipped++
			continue
		}
		row["state"] = "approved"
		row["status"] = "approved"
		row["updated_at"] = time.Now().UTC().Format(time.RFC3339)
		approved++
	}
	if approved > 0 {
		if err := writeJSONMap(path, m); err != nil {
			return err
		}
	}
	fmt.Printf("rule approve-all: %d approved, %d security-flagged skipped\n", approved, skipped)
	return nil
}

// RuleRejectReason rejects a proposal with an optional reason.
func (a *App) RuleRejectReason(id, reason string) error {
	return a.ruleDecide(id, false, reason)
}

func (a *App) ruleDecide(id string, approve bool, reason string) error {
	gw, err := a.currentGatewayURL()
	if err != nil || gw == "" {
		state := "rejected"
		if approve {
			state = "approved"
		}
		return a.ruleSet(id, state, reason)
	}
	c := cautem.NewWithToken(gw, a.gatewayTokenForURL(gw))
	ctx, cancel := a.withTimeout(TimeoutAPILong)
	defer cancel()

	sandbox := ""
	found := false
	for _, sb := range a.ruleCandidateSandboxes(ctx, c) {
		pp, err := c.GetProposal(ctx, sb, id)
		if err == nil && pp.ID == id {
			sandbox = sb
			found = true
			break
		}
	}
	if !found {
		state := "rejected"
		if approve {
			state = "approved"
		}
		return a.ruleSet(id, state, reason)
	}

	if approve {
		p, err := c.ApproveProposal(ctx, sandbox, id)
		if err != nil {
			return err
		}
		if err := a.refreshApprovedPolicy(ctx, c, sandbox); err != nil {
			return err
		}
		if a.Docker != nil {
			hostPath, err := a.Docker.PolicyHostPath(ctx, sandbox)
			if err != nil {
				return err
			}
			fmt.Printf("rule approve: ok id=%s sandbox=%s rule=%s policy=%s\n", id, sandbox, p.RuleName, hostPath)
		} else {
			fmt.Printf("rule approve: ok id=%s sandbox=%s (no docker bind)\n", id, sandbox)
		}
		return nil
	}
	if _, err := c.RejectProposal(ctx, sandbox, id, reason); err != nil {
		return err
	}
	fmt.Printf("rule reject: ok id=%s sandbox=%s reason=%q\n", id, sandbox, reason)
	return nil
}

func (a *App) refreshApprovedPolicy(ctx context.Context, c *cautem.Client, sandbox string) error {
	eff, err := c.GetSandboxPolicy(ctx, sandbox, "full")
	if err != nil {
		return fmt.Errorf("rule approve: get effective: %w", err)
	}
	if a.Docker == nil {
		return nil
	}
	hostPath, err := a.Docker.PolicyHostPath(ctx, sandbox)
	if err != nil {
		return fmt.Errorf("rule approve: gateway ok, live bind: %w", err)
	}
	return writeFileInPlace(hostPath, eff)
}

func (a *App) ruleCandidateSandboxes(ctx context.Context, c *cautem.Client) []string {
	var out []string
	if list, err := c.ListSandboxes(ctx); err == nil {
		for _, sb := range list {
			out = append(out, sb.Name)
		}
	}
	if len(out) == 0 && a.Sandboxes != nil {
		if infos, err := a.ListSandboxes(); err == nil {
			for _, i := range infos {
				out = append(out, i.Name)
			}
		}
	}
	return out
}
