// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const MaterialPrefix = "material:"

const WaitDepType = "wait"

const (
	waitPRMergedPrefix = "wait:pr-merged:"
	waitCIGreenPrefix  = "wait:ci-green:"
)

var waitTargetPrefixes = []string{waitPRMergedPrefix, waitCIGreenPrefix, MaterialPrefix}

func WaitTargets(labels []string) []string {
	var out []string
	for _, l := range labels {
		for _, p := range waitTargetPrefixes {
			if x, ok := strings.CutPrefix(l, p); ok && x != "" {
				out = append(out, x)
			}
		}
	}
	return out
}

type WaitTargetNoOwnerError struct {
	Target string
}

func (e *WaitTargetNoOwnerError) Error() string {
	return fmt.Sprintf("wait target %q has no owner; set it as an external ref on the Bead that produces it first", e.Target)
}

type GateMaterialRequiredError struct{}

func (e *GateMaterialRequiredError) Error() string {
	return "a human/confirm Gate that mentions a PR needs --material <ref>"
}

type waitGraph struct {
	ctx       context.Context
	tx        *sql.Tx
	owners    map[string][]string
	waitEdges map[[2]string]bool
}

type idList struct {
	id   string
	list []string
}

func openLists(ctx context.Context, tx *sql.Tx, column, skipType string) ([]idList, error) {
	// #nosec G202
	rows, err := tx.QueryContext(ctx, `SELECT id, `+column+` FROM beads WHERE `+column+` IS NOT NULL AND status NOT IN (?, ?) AND type != ? ORDER BY id`, StatusClosed, StatusCancelled, skipType)
	if err != nil {
		return nil, fmt.Errorf("wait edges: query %s: %w", column, err)
	}
	defer func() { _ = rows.Close() }()
	var out []idList
	for rows.Next() {
		var e idList
		var raw sql.NullString
		err := rows.Scan(&e.id, &raw)
		if err == nil {
			e.list, err = decodeStringList(raw)
		}
		if err != nil {
			return nil, fmt.Errorf("wait edges: read %s: %w", column, err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (g *waitGraph) ownersOf(ref string) ([]string, error) {
	if g.owners == nil {
		all, err := openLists(g.ctx, g.tx, "external_refs", BeadTypeGate)
		if err != nil {
			return nil, err
		}
		g.owners = map[string][]string{}
		for _, e := range all {
			for _, r := range e.list {
				g.owners[r] = append(g.owners[r], e.id)
			}
		}
	}
	return g.owners[ref], nil
}

func (g *waitGraph) waitSuccessors(id string) ([]string, error) {
	var beadType, status string
	var labels sql.NullString
	err := g.tx.QueryRowContext(g.ctx, `SELECT type, status, labels FROM beads WHERE id = ?`, id).
		Scan(&beadType, &status, &labels)
	if err != nil || beadType != BeadTypeGate || IsTerminalStatus(status) {
		return nil, err
	}
	ls, err := decodeStringList(labels)
	var out []string
	for _, x := range WaitTargets(ls) {
		o, oerr := g.ownersOf(x)
		if oerr != nil {
			return nil, oerr
		}
		for _, p := range o {
			if g.waitEdges == nil {
				g.waitEdges = map[[2]string]bool{}
			}
			g.waitEdges[[2]string{id, p}] = true
		}
		out = append(out, o...)
	}
	return out, err
}

func (g *waitGraph) usedWait(path []string) bool {
	for i := 1; g != nil && i < len(path); i++ {
		if g.waitEdges[[2]string{path[i-1], path[i]}] {
			return true
		}
	}
	return false
}

func checkNewWaitTargets(ctx context.Context, tx *sql.Tx, gateID string, before, after []string) error {
	old := WaitTargets(before)
	g := &waitGraph{ctx: ctx, tx: tx}
	for _, x := range WaitTargets(after) {
		if slices.Contains(old, x) {
			continue
		}
		owners, err := g.ownersOf(x)
		if err != nil {
			return err
		}
		if len(owners) == 0 {
			return &WaitTargetNoOwnerError{Target: x}
		}
		for _, p := range owners {
			if err := rejectWaitCycle(ctx, tx, g, gateID, p); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkNewOwnedRefs(ctx context.Context, tx *sql.Tx, ownerID, status string, before, after []string) error {
	added := slices.DeleteFunc(slices.Clone(after), func(r string) bool { return slices.Contains(before, r) })
	if IsTerminalStatus(status) || len(added) == 0 {
		return nil
	}
	gates, err := openLists(ctx, tx, "labels", "")
	if err != nil {
		return err
	}
	g := &waitGraph{ctx: ctx, tx: tx}
	for _, gate := range gates {
		targets := WaitTargets(gate.list)
		if !slices.ContainsFunc(added, func(r string) bool { return slices.Contains(targets, r) }) {
			continue
		}
		if err := rejectWaitCycle(ctx, tx, g, gate.id, ownerID); err != nil {
			return err
		}
	}
	return nil
}

func rejectWaitCycle(ctx context.Context, tx *sql.Tx, g *waitGraph, gateID, ownerID string) error {
	path, err := findPath(ctx, tx, g, ownerID, gateID, BlocksDepType)
	if err != nil || path == nil {
		return err
	}
	display, err := displayIDsFor(ctx, tx, path)
	if err != nil {
		return err
	}
	return &CycleError{DepType: WaitDepType, Cycle: display}
}

var prMentionRE = regexp.MustCompile(`(?i)\bPRs?\b|\bmerg|pull/`)

func checkGateMaterial(description string, before, after []string) error {
	if !slices.ContainsFunc(after, isPersonKindLabel) || slices.ContainsFunc(after, isMaterialLabel) {
		return nil
	}
	newlyPerson := slices.ContainsFunc(after, func(l string) bool { return isPersonKindLabel(l) && !slices.Contains(before, l) })
	if !newlyPerson && !slices.ContainsFunc(before, isMaterialLabel) {
		return nil
	}
	f := ParseGateDescription(description)
	if !prMentionRE.MatchString(f.Subject + "\n" + f.Proposal + "\n" + f.Check) {
		return nil
	}
	return &GateMaterialRequiredError{}
}

func isMaterialLabel(l string) bool { return strings.HasPrefix(l, MaterialPrefix) }
