// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const (
	BlocksDepType = "blocks"

	DiscoveredFromDepType = "discovered-from"
	DuplicatesDepType     = "duplicates"
)

var ValidDepTypes = map[string]bool{
	BlocksDepType:         true,
	ParentChildDepType:    true,
	DiscoveredFromDepType: true,
	DuplicatesDepType:     true,
}

const DiscoveredFromFanoutWarnThreshold = 5

func CountActiveLinks(ctx context.Context, q Querier, dependsOnID, depType string) (int, error) {
	var n int
	err := q.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM dependencies WHERE depends_on_id = ? AND type = ? AND removed = 0`,
		dependsOnID, depType,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count active links: %w", err)
	}
	return n, nil
}

type SelfLinkError struct {
	ID string
}

func (e *SelfLinkError) Error() string {
	return e.ID + " cannot link to itself"
}

type CycleError struct {
	DepType string
	Cycle   []string
}

func (e *CycleError) Error() string {
	return fmt.Sprintf("adding this %s link would create a cycle: %s", e.DepType, strings.Join(e.Cycle, " -> "))
}

type DuplicateParentError struct {
	BeadID          string
	CurrentParentID string
}

func (e *DuplicateParentError) Error() string {
	return fmt.Sprintf("%s already has an active parent (%s); remove that link before adding a new one", e.BeadID, e.CurrentParentID)
}

type LinkNotFoundError struct {
	BeadID      string
	DependsOnID string
	DepType     string
}

func (e *LinkNotFoundError) Error() string {
	return fmt.Sprintf("no active %s link from %s to %s", e.DepType, e.BeadID, e.DependsOnID)
}

type TerminalBlockerError struct {
	IDs []string
}

func (e *TerminalBlockerError) Error() string {
	return fmt.Sprintf("blocks prerequisite already terminal: %s; 前提は既に終端。張らずに進めるか、前提を --reopen してから張る", strings.Join(e.IDs, ", "))
}

func checkBlockersNotTerminal(ctx context.Context, tx *sql.Tx, ids []string) error {
	var terminal []string
	for _, id := range ids {
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM beads WHERE id = ?`, id).Scan(&status); err != nil {
			return fmt.Errorf("check blocker status: %w", err)
		}
		if IsTerminalStatus(status) {
			terminal = append(terminal, id)
		}
	}
	if len(terminal) == 0 {
		return nil
	}
	display, err := displayIDsFor(ctx, tx, terminal)
	if err != nil {
		return err
	}
	return &TerminalBlockerError{IDs: display}
}

func AddLink(ctx context.Context, tx *sql.Tx, actor, beadID, dependsOnID, depType, occurredAt, reason string) error {
	if err := checkLinkConstraints(ctx, tx, beadID, dependsOnID, depType); err != nil {
		return err
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO dependencies (bead_id, depends_on_id, type, created_at, removed)
		VALUES (?, ?, ?, ?, 0)`,
		beadID, dependsOnID, depType, occurredAt)
	if err != nil {
		return fmt.Errorf("add link: insert: %w", err)
	}

	return Audit(ctx, tx, AuditRecord{
		OccurredAt: occurredAt,
		Actor:      actor,
		BeadID:     beadID,
		Kind:       AuditKindField,
		Field:      FieldDependency,
		NewValue:   fmt.Sprintf(`{"depends_on_id":%q,"type":%q,"removed":false}`, dependsOnID, depType),
		Reason:     reason,
	})
}

func UpsertLink(ctx context.Context, tx *sql.Tx, actor, beadID, dependsOnID, depType, occurredAt, reason string) error {
	if depType == BlocksDepType {
		if err := checkBlockersNotTerminal(ctx, tx, []string{dependsOnID}); err != nil {
			return err
		}
	}
	if err := upsertLink(ctx, tx, actor, beadID, dependsOnID, depType, occurredAt, reason); err != nil {
		return err
	}
	if depType != ParentChildDepType {
		return nil
	}
	return checkMilestoneOrder(ctx, tx, beadID)
}

func upsertLink(ctx context.Context, tx *sql.Tx, actor, beadID, dependsOnID, depType, occurredAt, reason string) error {
	if beadID == dependsOnID {
		return newSelfLinkError(ctx, tx, beadID)
	}

	var removed bool
	err := tx.QueryRowContext(ctx, `
		SELECT removed FROM dependencies WHERE bead_id = ? AND depends_on_id = ? AND type = ?`,
		beadID, dependsOnID, depType,
	).Scan(&removed)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return AddLink(ctx, tx, actor, beadID, dependsOnID, depType, occurredAt, reason)
	case err != nil:
		return fmt.Errorf("upsert link: lookup: %w", err)
	case !removed:
		return nil
	}

	if err := checkLinkConstraints(ctx, tx, beadID, dependsOnID, depType); err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE dependencies SET removed = 0, removed_set_at = ?
		WHERE bead_id = ? AND depends_on_id = ? AND type = ?`,
		occurredAt, beadID, dependsOnID, depType)
	if err != nil {
		return fmt.Errorf("upsert link: reactivate: %w", err)
	}

	return Audit(ctx, tx, AuditRecord{
		OccurredAt: occurredAt,
		Actor:      actor,
		BeadID:     beadID,
		Kind:       AuditKindField,
		Field:      FieldDependency,
		NewValue:   fmt.Sprintf(`{"depends_on_id":%q,"type":%q,"removed":false}`, dependsOnID, depType),
		Reason:     reason,
	})
}

func RemoveLink(ctx context.Context, tx *sql.Tx, actor, beadID, dependsOnID, depType, occurredAt, reason string) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE dependencies SET removed = 1, removed_set_at = ?
		WHERE bead_id = ? AND depends_on_id = ? AND type = ? AND removed = 0`,
		occurredAt, beadID, dependsOnID, depType)
	if err != nil {
		return fmt.Errorf("remove link: update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("remove link: rows affected: %w", err)
	}
	if n == 0 {
		return &LinkNotFoundError{BeadID: beadID, DependsOnID: dependsOnID, DepType: depType}
	}

	return Audit(ctx, tx, AuditRecord{
		OccurredAt: occurredAt,
		Actor:      actor,
		BeadID:     beadID,
		Kind:       AuditKindField,
		Field:      FieldDependency,
		NewValue:   fmt.Sprintf(`{"depends_on_id":%q,"type":%q,"removed":true}`, dependsOnID, depType),
		Reason:     reason,
	})
}

func checkLinkConstraints(ctx context.Context, tx *sql.Tx, beadID, dependsOnID, depType string) error {
	if beadID == dependsOnID {
		return newSelfLinkError(ctx, tx, beadID)
	}

	if depType == BlocksDepType || depType == ParentChildDepType {
		var g *waitGraph
		if depType == BlocksDepType {
			g = &waitGraph{ctx: ctx, tx: tx}
		}
		cycle, err := findPath(ctx, tx, g, dependsOnID, beadID, depType)
		if err != nil {
			return err
		}
		if cycle != nil {
			displayCycle, derr := displayIDsFor(ctx, tx, cycle)
			if derr != nil {
				return derr
			}
			cycleType := depType
			if g.usedWait(cycle) {
				cycleType = WaitDepType
			}
			return &CycleError{DepType: cycleType, Cycle: displayCycle}
		}
	}

	if depType == ParentChildDepType {
		var currentParentID string
		err := tx.QueryRowContext(ctx, `
			SELECT depends_on_id FROM dependencies
			WHERE bead_id = ? AND type = ? AND removed = 0`,
			beadID, ParentChildDepType,
		).Scan(&currentParentID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return fmt.Errorf("check parent uniqueness: %w", err)
		default:
			display, derr := DisplayIDFor(ctx, tx, currentParentID)
			if derr != nil {
				return derr
			}
			beadDisplay, ierr := DisplayIDFor(ctx, tx, beadID)
			if ierr != nil {
				return ierr
			}
			return &DuplicateParentError{BeadID: beadDisplay, CurrentParentID: display}
		}
		return checkParentChildTarget(ctx, tx, beadID, dependsOnID)
	}

	return nil
}

func findPath(ctx context.Context, tx *sql.Tx, g *waitGraph, from, to, depType string) ([]string, error) {
	if from == to {
		return []string{from}, nil
	}

	parent := map[string]string{from: ""}
	queue := []string{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		rows, err := tx.QueryContext(ctx, `
			SELECT depends_on_id FROM dependencies
			WHERE bead_id = ? AND type = ? AND removed = 0`, cur, depType)
		if err != nil {
			return nil, fmt.Errorf("find path: query: %w", err)
		}
		next, err := collectRows(rows, scanString)
		if err != nil {
			return nil, fmt.Errorf("find path: iterate: %w", err)
		}
		if g != nil {
			waits, err := g.waitSuccessors(cur)
			if err != nil {
				return nil, err
			}
			next = append(next, waits...)
		}

		for _, n := range next {
			if _, seen := parent[n]; seen {
				continue
			}
			parent[n] = cur
			if n == to {
				path := []string{n}
				for at := cur; at != ""; at = parent[at] {
					path = append([]string{at}, path...)
				}
				return path, nil
			}
			queue = append(queue, n)
		}
	}
	return nil, nil
}

func DisplayIDFor(ctx context.Context, tx *sql.Tx, id string) (string, error) {
	var namespace string
	if err := tx.QueryRowContext(ctx, `SELECT namespace FROM beads WHERE id = ?`, id).Scan(&namespace); err != nil {
		return "", fmt.Errorf("display id: lookup namespace: %w", err)
	}
	n, err := LoadShortIDs(ctx, tx)
	if err != nil {
		return "", err
	}
	return DisplayID(namespace, id, n), nil
}

func displayIDsFor(ctx context.Context, tx *sql.Tx, ids []string) ([]string, error) {
	out := make([]string, len(ids))
	for i, id := range ids {
		display, err := DisplayIDFor(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		out[i] = display
	}
	return out, nil
}

func newSelfLinkError(ctx context.Context, tx *sql.Tx, id string) error {
	display, err := DisplayIDFor(ctx, tx, id)
	if err != nil {
		return err
	}
	return &SelfLinkError{ID: display}
}
