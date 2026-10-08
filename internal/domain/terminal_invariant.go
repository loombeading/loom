// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
)

const (
	hintUnfinishedChildren     = "lm dep remove <子> <親> --type parent-child で外し、lm dep add <子> <親> --type discovered-from で結ぶか、子を先に終端化する"
	hintUnfinishedDependents   = "下流を lm update --status cancelled にするか、lm dep remove と lm dep add で前提を張り替える"
	hintTerminalParentLink     = "親を先に lm update <親> --reopen で戻すか、lm dep add <子> <親> --type discovered-from で結ぶ"
	hintTerminalParentReopen   = "親を先に lm update <親> --reopen で戻す"
	hintCancelledPrerequisites = "前提を lm dep remove <id> <前提> --type blocks で外すか、前提を先に lm update <前提> --reopen で戻す"
)

type TerminalInvariantError struct {
	Reason string
	Hint   string
}

func (e *TerminalInvariantError) Error() string { return e.Reason }

type unfinishedLink struct {
	id, createdAt string
	priority      int
}

func unfinishedLinkedTo(ctx context.Context, tx *sql.Tx, depType string, targets []string) ([]string, error) {
	var found []unfinishedLink
	for _, target := range targets {
		rows, err := tx.QueryContext(ctx, `
			SELECT b.id, b.priority, b.created_at
			FROM dependencies d
			JOIN beads b ON b.id = d.bead_id
			WHERE d.depends_on_id = ? AND d.type = ? AND d.removed = 0 AND b.status IN (?, ?)`,
			target, depType, StatusOpen, StatusInProgress)
		if err != nil {
			return nil, fmt.Errorf("terminal invariant: query %s: %w", depType, err)
		}
		links, err := collectRows(rows, func(r *sql.Rows, l *unfinishedLink) error { return r.Scan(&l.id, &l.priority, &l.createdAt) })
		if err != nil {
			return nil, fmt.Errorf("terminal invariant: iterate %s: %w", depType, err)
		}
		found = append(found, links...)
	}
	slices.SortFunc(found, func(a, b unfinishedLink) int {
		return cmp.Or(cmp.Compare(a.priority, b.priority), cmp.Compare(a.createdAt, b.createdAt), cmp.Compare(a.id, b.id))
	})
	var out []string
	for _, l := range found {
		if !slices.Contains(targets, l.id) && !slices.Contains(out, l.id) {
			out = append(out, l.id)
		}
	}
	return out, nil
}

func checkTerminalInvariant(ctx context.Context, tx *sql.Tx, depType, label, hint string, targets []string) error {
	ids, err := unfinishedLinkedTo(ctx, tx, depType, targets)
	if err != nil || len(ids) == 0 {
		return err
	}
	return newTerminalInvariantError(ctx, tx, label, hint, ids)
}

func newTerminalInvariantError(ctx context.Context, tx *sql.Tx, label, hint string, ids []string) error {
	display, err := displayIDsFor(ctx, tx, ids)
	if err != nil {
		return err
	}
	resolver, err := NewAliasResolverFor(ctx, tx, ids)
	if err != nil {
		return err
	}
	for i, id := range ids {
		if alias, hasParent, err := resolver.Alias(id); err != nil {
			return err
		} else if hasParent {
			display[i] = alias
		}
	}
	return &TerminalInvariantError{Reason: label + ": " + strings.Join(display, ", "), Hint: hint}
}

func checkNoUnfinishedChildren(ctx context.Context, tx *sql.Tx, id string) error {
	return checkTerminalInvariant(ctx, tx, ParentChildDepType, "unfinished children", hintUnfinishedChildren, []string{id})
}

func checkNoUnfinishedDependents(ctx context.Context, tx *sql.Tx, ids []string) error {
	return checkTerminalInvariant(ctx, tx, BlocksDepType, "unfinished dependents", hintUnfinishedDependents, ids)
}

func checkParentChildTarget(ctx context.Context, tx *sql.Tx, childID, parentID string) error {
	var childStatus, parentStatus string
	err := tx.QueryRowContext(ctx, `
		SELECT c.status, p.status FROM beads c, beads p WHERE c.id = ? AND p.id = ?`,
		childID, parentID,
	).Scan(&childStatus, &parentStatus)
	if err != nil {
		return fmt.Errorf("terminal invariant: parent status: %w", err)
	}
	if IsTerminalStatus(childStatus) || !IsTerminalStatus(parentStatus) {
		return nil
	}
	return newTerminalInvariantError(ctx, tx, "terminal parent", hintTerminalParentLink, []string{parentID})
}

func checkReopenTarget(ctx context.Context, tx *sql.Tx, id string) error {
	parents, err := linkedPrerequisites(ctx, tx, ParentChildDepType, id, StatusClosed, StatusCancelled)
	if err != nil {
		return err
	}
	if len(parents) > 0 {
		return newTerminalInvariantError(ctx, tx, "terminal parent", hintTerminalParentReopen, parents)
	}
	prereqs, err := linkedPrerequisites(ctx, tx, BlocksDepType, id, StatusCancelled, StatusCancelled)
	if err != nil || len(prereqs) == 0 {
		return err
	}
	return newTerminalInvariantError(ctx, tx, "cancelled prerequisites", hintCancelledPrerequisites, prereqs)
}

func linkedPrerequisites(ctx context.Context, tx *sql.Tx, depType, id, status1, status2 string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT b.id FROM dependencies d
		JOIN beads b ON b.id = d.depends_on_id
		WHERE d.bead_id = ? AND d.type = ? AND d.removed = 0 AND b.status IN (?, ?)
		ORDER BY b.priority, b.created_at, b.id`,
		id, depType, status1, status2)
	if err != nil {
		return nil, fmt.Errorf("terminal invariant: query %s prerequisites: %w", depType, err)
	}
	ids, err := collectRows(rows, func(r *sql.Rows, s *string) error { return r.Scan(s) })
	if err != nil {
		return nil, fmt.Errorf("terminal invariant: iterate %s prerequisites: %w", depType, err)
	}
	return ids, nil
}
