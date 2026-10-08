// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func checkMilestoneOrder(ctx context.Context, tx *sql.Tx, rootID string) error {
	var id, msID string
	var priority, msPriority int
	err := tx.QueryRowContext(ctx, `
		WITH RECURSIVE sub(id) AS (
			SELECT ?
			UNION
			SELECT d.bead_id FROM dependencies d
			JOIN sub s ON d.depends_on_id = s.id
			WHERE d.type = ? AND d.removed = 0
		),
		anc(node, id, depth) AS (
			SELECT s.id, d.depends_on_id, 1 FROM sub s
			JOIN dependencies d ON d.bead_id = s.id
			WHERE d.type = ? AND d.removed = 0
			UNION ALL
			SELECT a.node, d.depends_on_id, a.depth + 1 FROM anc a
			JOIN dependencies d ON d.bead_id = a.id
			WHERE d.type = ? AND d.removed = 0
		),
		nearest(node, ms, depth) AS (
			SELECT a.node, a.id, MIN(a.depth) FROM anc a
			JOIN beads m ON m.id = a.id
			WHERE m.labels LIKE '%"' || ? || '%'
			GROUP BY a.node
		)
		SELECT n.id, n.priority, m.id, m.priority FROM nearest x
		JOIN beads n ON n.id = x.node
		JOIN beads m ON m.id = x.ms
		WHERE n.type != ? AND n.status NOT IN (?, ?) AND n.priority < m.priority
		ORDER BY n.id LIMIT 1`,
		rootID, ParentChildDepType, ParentChildDepType, ParentChildDepType, milestoneLabelPrefix,
		BeadTypeGate, StatusClosed, StatusCancelled).Scan(&id, &priority, &msID, &msPriority)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check milestone order: %w", err)
	}
	ids, err := displayIDsFor(ctx, tx, []string{id, msID})
	if err != nil {
		return err
	}
	return &ValidationError{Msg: fmt.Sprintf("%s の保存優先度 P%d が最も近いマイルストーン %s の P%d より高い。P%d 以下（--priority %d 以上）を選ぶ", ids[0], priority, ids[1], msPriority, msPriority, msPriority)}
}
