// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
)

const FieldGateResolve = "gate_resolve"

const (
	GateKindHuman      = "human"
	GateKindAdjudicate = "adjudicate"
	GateKindExternal   = "external"
	GateKindUnknown    = "unknown"
)

func GateKindOf(labels []string, await string) string {
	if slices.Contains(labels, "kind:external") {
		return GateKindExternal
	}
	if slices.Contains(labels, "kind:human") || slices.Contains(labels, "kind:confirm") {
		return GateKindHuman
	}
	if slices.Contains(labels, "kind:adjudicate") {
		return GateKindAdjudicate
	}
	return GateKindUnknown
}

type GateFields struct {
	Subject  string
	Current  string
	Proposal string
	Check    string
	Resolver string
}

const (
	gateFieldCurrentPrefix  = "現状: "
	gateFieldProposalPrefix = "提案: "
	gateFieldCheckPrefix    = "チェックすると: "
	gateFieldResolverPrefix = "解決者: "
)

func ComposeGateDescription(f GateFields) string {
	lines := []string{f.Subject}
	if f.Current != "" {
		lines = append(lines, gateFieldCurrentPrefix+f.Current)
	}
	if f.Proposal != "" {
		lines = append(lines, gateFieldProposalPrefix+f.Proposal)
	}
	if f.Check != "" {
		lines = append(lines, gateFieldCheckPrefix+f.Check)
	}
	if f.Resolver != "" {
		lines = append(lines, gateFieldResolverPrefix+f.Resolver)
	}
	return strings.Join(lines, "\n")
}

func ParseGateDescription(desc string) GateFields {
	lines := strings.Split(desc, "\n")
	var f GateFields
	if len(lines) > 0 {
		f.Subject = lines[0]
	}
	for _, line := range lines[1:] {
		switch {
		case strings.HasPrefix(line, gateFieldCurrentPrefix):
			f.Current = strings.TrimPrefix(line, gateFieldCurrentPrefix)
		case strings.HasPrefix(line, gateFieldProposalPrefix):
			f.Proposal = strings.TrimPrefix(line, gateFieldProposalPrefix)
		case strings.HasPrefix(line, gateFieldCheckPrefix):
			f.Check = strings.TrimPrefix(line, gateFieldCheckPrefix)
		case strings.HasPrefix(line, gateFieldResolverPrefix):
			f.Resolver = strings.TrimPrefix(line, gateFieldResolverPrefix)
		}
	}
	return f
}

type GateCreateInput struct {
	Blocks    []string
	Fields    GateFields
	Namespace string
	Labels    []string

	Actor  string
	Reason string
	Now    string
}

const gateTitleMaxRunes = 60

const gateTitleEllipsis = "…"

func GateTitle(subject string) string {
	runes := []rune(subject)
	if len(runes) <= gateTitleMaxRunes {
		return "gate: " + subject
	}
	return "gate: " + string(runes[:gateTitleMaxRunes]) + gateTitleEllipsis
}

func GateCreate(ctx context.Context, tx *sql.Tx, in GateCreateInput) (string, error) {
	if len(in.Blocks) == 0 {
		return "", &ValidationError{Msg: "lm gate create requires at least one --blocks ID"}
	}
	if in.Fields.Subject == "" {
		return "", &ValidationError{Msg: "--subject is required"}
	}
	if slices.Contains(in.Labels, "kind:external") && in.Fields.Resolver == "" {
		return "", &ValidationError{Msg: "--resolver is required for --kind external"}
	}

	blockedIDs := make([]string, 0, len(in.Blocks))
	for _, s := range in.Blocks {
		id, err := ResolveID(ctx, tx, s)
		if err != nil {
			return "", err
		}
		blockedIDs = append(blockedIDs, id)
	}

	gateID, err := CreateBead(ctx, tx, CreateInput{
		Title:       GateTitle(in.Fields.Subject),
		Description: ComposeGateDescription(in.Fields),
		Priority:    DefaultPriority,
		BeadType:    BeadTypeGate,
		Namespace:   in.Namespace,
		Labels:      in.Labels,
		Actor:       in.Actor,
		Reason:      in.Reason,
		Now:         in.Now,
	})
	if err != nil {
		return "", err
	}
	if err := checkNewWaitTargets(ctx, tx, gateID, nil, in.Labels); err != nil {
		return "", err
	}

	for _, blockedID := range blockedIDs {
		if err := AddLink(ctx, tx, in.Actor, blockedID, gateID, BlocksDepType, in.Now, in.Reason); err != nil {
			return "", err
		}
	}

	return gateID, nil
}

type GateResolveInput struct {
	ID     string
	Reason string
	Actor  string
	Now    string
}

func GateResolve(ctx context.Context, tx *sql.Tx, in GateResolveInput) error {
	if in.Reason == "" {
		return &ValidationError{Msg: "--reason is required for `lm gate resolve`"}
	}

	status, beadType, err := lookupStatusAndType(ctx, tx, in.ID, "gate resolve")
	if err != nil {
		return err
	}
	if beadType != BeadTypeGate {
		return &ValidationError{Msg: fmt.Sprintf("%s is not a gate Bead (type = %q)", in.ID, beadType)}
	}
	if !CanTransition(status, StatusClosed) {
		return &ValidationError{Msg: fmt.Sprintf("cannot resolve a gate in status %q", status)}
	}

	if err := execUpdate(ctx, tx, in.ID, map[string]any{colStatus: StatusClosed, colClosedAt: in.Now}, in.Now); err != nil {
		return err
	}
	return Audit(ctx, tx, AuditRecord{
		OccurredAt: in.Now, Actor: in.Actor, BeadID: in.ID,
		Kind: AuditKindField, Field: FieldGateResolve, NewValue: StatusClosed, Reason: in.Reason,
	})
}

const FieldGateReject = "gate_reject"

type GateRejectInput struct {
	ID     string
	Reason string
	Actor  string
	Now    string
}

func GateReject(ctx context.Context, tx *sql.Tx, in GateRejectInput) ([]string, error) {
	if in.Reason == "" {
		return nil, &ValidationError{Msg: "--reason is required for `lm gate reject`"}
	}

	status, beadType, err := lookupStatusAndType(ctx, tx, in.ID, "gate reject")
	if err != nil {
		return nil, err
	}
	if beadType != BeadTypeGate {
		return nil, &ValidationError{Msg: fmt.Sprintf("%s is not a gate Bead (type = %q)", in.ID, beadType)}
	}
	if !CanTransition(status, StatusCancelled) {
		return nil, &ValidationError{Msg: fmt.Sprintf("cannot reject a gate in status %q", status)}
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT b.id, b.status
		FROM dependencies d
		JOIN beads b ON b.id = d.bead_id
		WHERE d.depends_on_id = ? AND d.type = ? AND d.removed = 0
		  AND b.status IN (?, ?)
		ORDER BY b.created_at ASC, b.id ASC`, in.ID, BlocksDepType, StatusOpen, StatusInProgress)
	if err != nil {
		return nil, fmt.Errorf("gate reject: query blocking: %w", err)
	}
	type blocked struct {
		id     string
		status string
	}
	toCancel, err := collectRows(rows, func(r *sql.Rows, b *blocked) error { return r.Scan(&b.id, &b.status) })
	if err != nil {
		return nil, fmt.Errorf("gate reject: iterate blocking: %w", err)
	}

	cancelledIDs := make([]string, 0, len(toCancel))
	for _, b := range toCancel {
		cancelledIDs = append(cancelledIDs, b.id)
	}
	if err := checkNoUnfinishedDependents(ctx, tx, append(slices.Clone(cancelledIDs), in.ID)); err != nil {
		return nil, err
	}
	for _, b := range toCancel {
		sets := map[string]any{colStatus: StatusCancelled, colClosedAt: in.Now}
		if b.status == StatusInProgress {
			sets["claimed_by"] = nil
			sets[colClaimExpiresAt] = nil
		}
		if err := execUpdate(ctx, tx, b.id, sets, in.Now); err != nil {
			return nil, err
		}
		if err := Audit(ctx, tx, AuditRecord{
			OccurredAt: in.Now, Actor: in.Actor, BeadID: b.id,
			Kind: AuditKindField, Field: colStatus, NewValue: StatusCancelled, Reason: in.Reason,
		}); err != nil {
			return nil, err
		}
	}

	if err := execUpdate(ctx, tx, in.ID, map[string]any{colStatus: StatusCancelled, colClosedAt: in.Now}, in.Now); err != nil {
		return nil, err
	}
	if err := Audit(ctx, tx, AuditRecord{
		OccurredAt: in.Now, Actor: in.Actor, BeadID: in.ID,
		Kind: AuditKindField, Field: FieldGateReject, NewValue: StatusCancelled, Reason: in.Reason,
	}); err != nil {
		return nil, err
	}

	return cancelledIDs, nil
}

type GateListEntry struct {
	ID        string
	Namespace string
	Priority  int
	CreatedAt string
	Title     string
	Fields    GateFields
	Labels    []string
	Blocking  []Blocker

	PrereqBlockers []Blocker
}

func GateListEffective(ctx context.Context, q Querier, now string) ([]GateListEntry, map[string]EffectivePriority, error) {
	raised, err := RaisedPriorities(ctx, q, nowOrDefault(now))
	if err != nil {
		return nil, nil, err
	}
	out, err := gateList(ctx, q)
	if err != nil {
		return nil, nil, err
	}
	slices.SortFunc(out, func(a, b GateListEntry) int {
		return compareEffective(raised,
			Bead{ID: a.ID, Priority: a.Priority, CreatedAt: a.CreatedAt},
			Bead{ID: b.ID, Priority: b.Priority, CreatedAt: b.CreatedAt})
	})
	breakPrereqCycles(out)
	return out, raised, nil
}

func gateList(ctx context.Context, q Querier) ([]GateListEntry, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, namespace, priority, created_at, title, description, labels
		FROM beads
		WHERE type = 'gate' AND status = ?`, StatusOpen)
	if err != nil {
		return nil, fmt.Errorf("gate list: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []GateListEntry
	for rows.Next() {
		var e GateListEntry
		var desc, labels sql.NullString
		if err := rows.Scan(&e.ID, &e.Namespace, &e.Priority, &e.CreatedAt, &e.Title, &desc, &labels); err != nil {
			return nil, fmt.Errorf("gate list: scan: %w", err)
		}
		if desc.Valid {
			e.Fields = ParseGateDescription(desc.String)
		}
		e.Labels, err = decodeStringList(labels)
		if err != nil {
			return nil, fmt.Errorf("gate list: decode labels: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("gate list: iterate: %w", err)
	}

	for i := range out {
		blocking, err := GateBlocking(ctx, q, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Blocking = blocking

		prereq, err := GatePrereqBlockers(ctx, q, out[i].ID, blocking)
		if err != nil {
			return nil, err
		}
		out[i].PrereqBlockers = prereq
	}
	return out, nil
}

type GateWaitCoord struct {
	ID        string
	Namespace string
	Status    string
}

type GateBlockerWait struct {
	GateWaitCoord

	Children []GateWaitCoord
}

func GateWaitProgress(ctx context.Context, q Querier, gateID string) ([]GateBlockerWait, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT b.id, b.namespace, b.status
		FROM dependencies d
		JOIN beads b ON b.id = d.bead_id
		WHERE d.depends_on_id = ? AND d.type = ? AND d.removed = 0
		ORDER BY b.created_at ASC, b.id ASC`, gateID, BlocksDepType)
	if err != nil {
		return nil, fmt.Errorf("gate wait progress: query blocked: %w", err)
	}
	out, err := collectRows(rows, func(r *sql.Rows, b *GateBlockerWait) error { return r.Scan(&b.ID, &b.Namespace, &b.Status) })
	if err != nil {
		return nil, fmt.Errorf("gate wait progress: iterate blocked: %w", err)
	}

	for i := range out {
		crows, err := q.QueryContext(ctx, `
			SELECT b.id, b.namespace, b.status
			FROM dependencies d
			JOIN beads b ON b.id = d.bead_id
			WHERE d.depends_on_id = ? AND d.type = ? AND d.removed = 0
			ORDER BY b.created_at ASC, b.id ASC`, out[i].ID, ParentChildDepType)
		if err != nil {
			return nil, fmt.Errorf("gate wait progress: query children: %w", err)
		}
		out[i].Children, err = collectRows(crows, func(r *sql.Rows, c *GateWaitCoord) error { return r.Scan(&c.ID, &c.Namespace, &c.Status) })
		if err != nil {
			return nil, fmt.Errorf("gate wait progress: iterate children: %w", err)
		}
	}
	return out, nil
}

func GateBlocking(ctx context.Context, q Querier, gateID string) ([]Blocker, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT b.id, b.namespace
		FROM dependencies d
		JOIN beads b ON b.id = d.bead_id
		WHERE d.depends_on_id = ? AND d.type = ? AND d.removed = 0
		ORDER BY b.created_at ASC, b.id ASC`, gateID, BlocksDepType)
	if err != nil {
		return nil, fmt.Errorf("gate blocking: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Blocker
	for rows.Next() {
		var bl Blocker
		if err := rows.Scan(&bl.ID, &bl.Namespace); err != nil {
			return nil, fmt.Errorf("gate blocking: scan: %w", err)
		}
		out = append(out, bl)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("gate blocking: iterate: %w", err)
	}
	return out, nil
}

func GatePrereqBlockers(ctx context.Context, q Querier, gateID string, blocking []Blocker) ([]Blocker, error) {
	seen := make(map[string]bool, len(blocking))
	var out []Blocker

	selfBlockers, err := ActiveBlockers(ctx, q, gateID)
	if err != nil {
		return nil, err
	}
	for _, b := range selfBlockers {
		seen[b.ID] = true
		out = append(out, b)
	}

	anyNonTerminal := false
	allHaveOthers := true
	var otherBlockers []Blocker
	for _, bl := range blocking {
		terminal, err := beadIsTerminal(ctx, q, bl.ID)
		if err != nil {
			return nil, err
		}
		if terminal {
			continue
		}
		anyNonTerminal = true

		blockers, err := ActiveBlockers(ctx, q, bl.ID)
		if err != nil {
			return nil, err
		}
		others := 0
		for _, b := range blockers {
			if b.ID == gateID {
				continue
			}
			others++
			otherBlockers = append(otherBlockers, b)
		}
		if others == 0 {
			allHaveOthers = false
		}
	}

	if anyNonTerminal && allHaveOthers {
		for _, b := range otherBlockers {
			if seen[b.ID] {
				continue
			}
			seen[b.ID] = true
			out = append(out, b)
		}
	}

	return out, nil
}

func beadIsTerminal(ctx context.Context, q Querier, id string) (bool, error) {
	rows, err := q.QueryContext(ctx, `SELECT 1 FROM beads WHERE id = ? AND status IN (?, ?)`, id, StatusClosed, StatusCancelled)
	if err != nil {
		return false, fmt.Errorf("bead is terminal: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return rows.Next(), rows.Err()
}

func breakPrereqCycles(entries []GateListEntry) {
	index := make(map[string]int, len(entries))
	for i, e := range entries {
		if len(e.PrereqBlockers) > 0 && GateKindOf(e.Labels, e.Fields.Subject) == GateKindHuman {
			index[e.ID] = i
		}
	}
	edges := func(i int) []int {
		var out []int
		for _, p := range entries[i].PrereqBlockers {
			if j, ok := index[p.ID]; ok {
				out = append(out, j)
			}
		}
		return out
	}

	low := map[int]int{}
	order := map[int]int{}
	onStack := map[int]bool{}
	var stack []int
	var components [][]int
	var visit func(v int)
	visit = func(v int) {
		order[v] = len(order)
		low[v] = order[v]
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range edges(v) {
			if _, done := order[w]; !done {
				visit(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], order[w])
			}
		}
		if low[v] != order[v] {
			return
		}
		var comp []int
		for {
			w := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[w] = false
			comp = append(comp, w)
			if w == v {
				break
			}
		}
		components = append(components, comp)
	}
	for i := range entries {
		if _, ok := index[entries[i].ID]; !ok {
			continue
		}
		if _, done := order[i]; !done {
			visit(i)
		}
	}
	for _, comp := range components {
		if len(comp) < 2 {
			continue
		}
		entries[slices.Min(comp)].PrereqBlockers = nil
	}
}
