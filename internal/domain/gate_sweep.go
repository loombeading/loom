// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"fmt"
)

const ReasonGateBlockedAllTerminal = "自動: 塞いだ Bead が全て終端"

const ReasonGateAdjudicateNoteHandoff = "自動: kind:adjudicate Gate 解除時の判定ノート引き継ぎ"

const gateAdjudicateNoteTitlePrefix = "判定ノート引き継ぎ: "

func adjudicateNoteTitle(subject string) string {
	runes := []rune(subject)
	if len(runes) <= gateTitleMaxRunes {
		return gateAdjudicateNoteTitlePrefix + subject
	}
	return gateAdjudicateNoteTitlePrefix + string(runes[:gateTitleMaxRunes]) + gateTitleEllipsis
}

type gateInfo struct {
	Namespace   string
	Description string
	Labels      []string
}

func lookupGateInfo(ctx context.Context, q Querier, gateID string) (gateInfo, error) {
	var g gateInfo
	var desc, labels sql.NullString
	err := q.QueryRowContext(ctx, `
		SELECT namespace, description, labels FROM beads WHERE id = ?`, gateID).
		Scan(&g.Namespace, &desc, &labels)
	if err != nil {
		return gateInfo{}, fmt.Errorf("lookup gate info: %w", err)
	}
	if desc.Valid {
		g.Description = desc.String
	}
	g.Labels, err = decodeStringList(labels)
	if err != nil {
		return gateInfo{}, fmt.Errorf("lookup gate info: decode labels: %w", err)
	}
	return g, nil
}

func gateHasDiscoveredFromBead(ctx context.Context, q Querier, gateID string) (bool, error) {
	var exists int
	err := q.QueryRowContext(ctx, `
		SELECT 1 FROM dependencies
		WHERE depends_on_id = ? AND type = ? AND removed = 0
		LIMIT 1`, gateID, DiscoveredFromDepType).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("gate has discovered-from bead: query: %w", err)
	}
	return true, nil
}

func spawnAdjudicateNoteHandoffIfNeeded(ctx context.Context, tx *sql.Tx, gateID, actor, now string) error {
	info, err := lookupGateInfo(ctx, tx, gateID)
	if err != nil {
		return err
	}
	fields := ParseGateDescription(info.Description)
	if GateKindOf(info.Labels, fields.Subject) != GateKindAdjudicate {
		return nil
	}
	already, err := gateHasDiscoveredFromBead(ctx, tx, gateID)
	if err != nil {
		return err
	}
	if already {
		return nil
	}
	_, err = CreateBead(ctx, tx, CreateInput{
		Title:          adjudicateNoteTitle(fields.Subject),
		Description:    info.Description,
		Priority:       DefaultPriority,
		BeadType:       "task",
		Namespace:      info.Namespace,
		DiscoveredFrom: gateID,
		Actor:          actor,
		Reason:         ReasonGateAdjudicateNoteHandoff,
		Now:            now,
	})
	return err
}

type gateBlockedBead struct {
	ID     string
	Status string
}

func gateBlockedBeads(ctx context.Context, q Querier, gateID string) ([]gateBlockedBead, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT b.id, b.status
		FROM dependencies d
		JOIN beads b ON b.id = d.bead_id
		WHERE d.depends_on_id = ? AND d.type = ? AND d.removed = 0
		ORDER BY b.created_at ASC, b.id ASC`, gateID, BlocksDepType)
	if err != nil {
		return nil, fmt.Errorf("gate blocked beads: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []gateBlockedBead
	for rows.Next() {
		var b gateBlockedBead
		if err := rows.Scan(&b.ID, &b.Status); err != nil {
			return nil, fmt.Errorf("gate blocked beads: scan: %w", err)
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("gate blocked beads: iterate: %w", err)
	}
	return out, nil
}

func evaluateGateWait(ctx context.Context, q Querier, gateID string) (bool, error) {
	blocked, err := gateBlockedBeads(ctx, q, gateID)
	if err != nil {
		return false, err
	}
	if len(blocked) == 0 {
		return false, nil
	}
	for _, b := range blocked {
		if !IsTerminalStatus(b.Status) {
			return false, nil
		}
	}
	return true, nil
}

func resolveGateIfWaitMet(ctx context.Context, tx *sql.Tx, gateID, actor, now string) (bool, error) {
	met, err := evaluateGateWait(ctx, tx, gateID)
	if err != nil {
		return false, err
	}
	if !met {
		return false, nil
	}
	if err := spawnAdjudicateNoteHandoffIfNeeded(ctx, tx, gateID, actor, now); err != nil {
		return false, err
	}
	if err := GateResolve(ctx, tx, GateResolveInput{ID: gateID, Reason: ReasonGateBlockedAllTerminal, Actor: actor, Now: now}); err != nil {
		return false, err
	}
	return true, nil
}

func AutoResolveGatesForBead(ctx context.Context, tx *sql.Tx, id, actor, now string) ([]string, error) {
	gateIDs, err := candidateGatesForBead(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	var resolved []string
	for _, gateID := range gateIDs {
		ok, err := resolveGateIfWaitMet(ctx, tx, gateID, actor, now)
		if err != nil {
			return nil, err
		}
		if ok {
			resolved = append(resolved, gateID)
		}
	}
	return resolved, nil
}

func candidateGatesForBead(ctx context.Context, tx *sql.Tx, id string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT g.id
		FROM dependencies d
		JOIN beads g ON g.id = d.depends_on_id
		WHERE d.type = ? AND d.removed = 0 AND g.type = 'gate' AND g.status = ?
		  AND d.bead_id IN (SELECT ?)
		ORDER BY g.id ASC`, BlocksDepType, StatusOpen, id)
	if err != nil {
		return nil, fmt.Errorf("candidate gates for bead: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var gateID string
		if err := rows.Scan(&gateID); err != nil {
			return nil, fmt.Errorf("candidate gates for bead: scan: %w", err)
		}
		out = append(out, gateID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("candidate gates for bead: iterate: %w", err)
	}
	return out, nil
}

type GateSweepResult struct {
	Resolved []string
}

func GateSweep(ctx context.Context, tx *sql.Tx, actor, now string) (GateSweepResult, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id
		FROM beads
		WHERE type = 'gate' AND status = ?
		ORDER BY updated_at DESC, id ASC`, StatusOpen)
	if err != nil {
		return GateSweepResult{}, fmt.Errorf("gate sweep: query open gates: %w", err)
	}
	gateIDs, err := collectRows(rows, scanString)
	if err != nil {
		return GateSweepResult{}, fmt.Errorf("gate sweep: iterate open gates: %w", err)
	}

	var result GateSweepResult
	for _, id := range gateIDs {
		met, err := evaluateGateWait(ctx, tx, id)
		if err != nil {
			return GateSweepResult{}, err
		}
		if !met {
			continue
		}
		if err := spawnAdjudicateNoteHandoffIfNeeded(ctx, tx, id, actor, now); err != nil {
			return GateSweepResult{}, err
		}
		if err := GateResolve(ctx, tx, GateResolveInput{ID: id, Reason: ReasonGateBlockedAllTerminal, Actor: actor, Now: now}); err != nil {
			return GateSweepResult{}, err
		}
		result.Resolved = append(result.Resolved, id)
	}
	return result, nil
}
