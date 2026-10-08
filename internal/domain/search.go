// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import "context"

type SearchFilter struct {
	Query string

	All       bool
	Namespace string
	ParentID  string

	UpdatedBefore string
	UpdatedAfter  string

	Limit int
}

func SearchBeads(ctx context.Context, q Querier, f SearchFilter) ([]Bead, int, error) {
	pattern := "%" + escapeLike(f.Query) + "%"

	fromWhere := `FROM beads WHERE 1=1`
	var args []any
	if !f.All {
		fromWhere += ` AND status NOT IN (?, ?)`
		args = append(args, StatusClosed, StatusCancelled)
	}
	fromWhere += ` AND (
		title LIKE ? ESCAPE '\'
		OR description LIKE ? ESCAPE '\'
		OR summary LIKE ? ESCAPE '\'
		OR external_refs LIKE ? ESCAPE '\'
		OR EXISTS (
			SELECT 1 FROM audit_log a
			WHERE a.bead_id = beads.id AND a.reason LIKE ? ESCAPE '\'
		)
	)`
	args = append(args, pattern, pattern, pattern, pattern, pattern)
	fromWhere, args = commonListFilterConds(fromWhere, args, f.Namespace, f.ParentID, directChildOfClause, f.UpdatedBefore, f.UpdatedAfter)

	return runListQuery(ctx, q, fromWhere, args, "", f.Limit, SortUpdated)
}
