// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

const (
	SourceAged    = "aged"
	SourceDecayed = "decayed"
)

type adjustParams struct {
	promote, promoteGate, decay, hold time.Duration
	ceiling, floor                    [MaxSeverity + 1]int
}

func (c Coefficients) adjustParams() adjustParams {
	p := adjustParams{
		promote:     c.Duration("promote_after"),
		promoteGate: c.Duration("promote_after_gate"),
		decay:       c.Duration("decay_after"),
		hold:        c.Duration("hold_after"),
	}
	for n := 1; n <= MaxSeverity; n++ {
		p.ceiling[n] = c.Int(fmt.Sprintf("severity_%d_ceiling", n))
		p.floor[n] = c.Int(fmt.Sprintf("severity_%d_floor", n))
	}
	return p
}

func holdable(severity int) bool { return severity >= 3 }

type adjustInput struct {
	Graph, Stored, Severity int
	Demand, Gate            bool
	ReadySince, Progress    time.Time
	Now                     time.Time
}

type adjustResult struct {
	Priority int
	Source   string
	Held     bool
	HeldFrom time.Time
}

func steps(elapsed, every time.Duration) int {
	if elapsed <= 0 || every <= 0 {
		return 0
	}
	return int(elapsed / every)
}

func adjust(p adjustParams, in adjustInput) adjustResult {
	res := adjustResult{Priority: in.Graph}
	if in.Graph == 0 {
		return res
	}
	sev := min(max(in.Severity, 1), MaxSeverity)
	if in.Demand {
		every := p.promote
		if in.Gate {
			every = p.promoteGate
		}
		if ceiling := p.ceiling[sev]; in.Graph > ceiling {
			res.Priority = max(in.Graph-steps(in.Now.Sub(in.ReadySince), every), ceiling)
			if res.Priority < in.Graph {
				res.Source = SourceAged
			}
		}
		return res
	}
	floor := p.floor[sev]
	if floor == 0 {
		return res
	}
	if in.Graph < floor {
		res.Priority = min(in.Graph+steps(in.Now.Sub(in.Progress), p.decay), floor)
		if res.Priority > in.Graph {
			res.Source = SourceDecayed
		}
	}
	if holdable(sev) {
		atFloor := in.Progress.Add(time.Duration(max(floor-in.Stored, 0)) * p.decay)
		res.HeldFrom = atFloor.Add(p.hold)
		res.Held = !in.Now.Before(res.HeldFrom)
	}
	return res
}

type TimeAdjustment struct {
	Eff    map[string]EffectivePriority
	Demand map[string]bool
	Held   map[string]bool

	HeldSince map[string]time.Time
	HeldFrom  map[string]time.Time
}

type beadTimes struct {
	typ                      string
	severity, priority       int
	created, due, redetected string
	readySince, progress     Stamp
}

type Stamp struct {
	Time     time.Time
	Kind, ID string
}

func parseStamp(s sql.NullString) time.Time {
	if !s.Valid {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s.String)
	if err != nil {
		return time.Time{}
	}
	return t
}

func latestStamp(ss ...Stamp) Stamp {
	var out Stamp
	for _, s := range ss {
		if s.Time.After(out.Time) {
			out = s
		}
	}
	return out
}

type keyedValue struct{ key, id string }

type extremes struct {
	m   map[string]keyedValue
	min bool
}

func (e extremes) add(cand string, key sql.NullString, id string) {
	if !key.Valid {
		return
	}
	if cur, ok := e.m[cand]; ok && (cur.key == key.String || (cur.key < key.String) == e.min) {
		return
	}
	e.m[cand] = keyedValue{key.String, id}
}

func (e extremes) stamps(kind string) map[string]Stamp {
	out := make(map[string]Stamp, len(e.m))
	for cand, v := range e.m {
		out[cand] = Stamp{Time: parseStamp(sql.NullString{String: v.key, Valid: true}), Kind: kind, ID: v.id}
	}
	return out
}

func (e extremes) firsts() map[string]string {
	out := make(map[string]string, len(e.m))
	for cand, v := range e.m {
		out[cand] = v.id
	}
	return out
}

type linkFacts struct {
	closedPrereq, removedBlocks, closedChildren, touched, openDownstream extremes
}

func terminalStatus(s sql.NullString) bool {
	return s.Valid && (s.String == StatusClosed || s.String == StatusCancelled)
}

func linkTouchedAt(created, removedSet sql.NullString) sql.NullString {
	if created.Valid && created.String < removedSet.String {
		return removedSet
	}
	return created
}

func linkFactsOf(ctx context.Context, q Querier, ids []any, in string) (linkFacts, error) {
	newMax := func() extremes { return extremes{m: map[string]keyedValue{}} }
	lf := linkFacts{newMax(), newMax(), newMax(), newMax(), extremes{m: map[string]keyedValue{}, min: true}}
	scan := func(query string, args []any, each func(cand, other, typ string, removed bool, removedSet, status, closed, created sql.NullString)) error {
		rows, err := q.QueryContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("time adjust: query links: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var cand, other, typ string
			var removed bool
			var created, removedSet, status, closed, otherCreated sql.NullString
			if err := rows.Scan(&cand, &other, &typ, &removed, &created, &removedSet, &status, &closed, &otherCreated); err != nil {
				return fmt.Errorf("time adjust: scan links: %w", err)
			}
			lf.touched.add(cand, linkTouchedAt(created, removedSet), "")
			each(cand, other, typ, removed, removedSet, status, closed, otherCreated)
		}
		return rows.Err()
	}
	err := scan(`
		SELECT d.bead_id, d.depends_on_id, d.type, d.removed, d.created_at, d.removed_set_at, b.status, b.closed_at, b.created_at
		FROM dependencies d LEFT JOIN beads b ON b.id = d.depends_on_id AND d.type = ? AND d.removed = 0
		WHERE d.bead_id IN (`+in+`)`, slices.Concat([]any{BlocksDepType}, ids),
		func(cand, other, typ string, removed bool, removedSet, status, closed, _ sql.NullString) {
			switch {
			case typ != BlocksDepType:
			case removed:
				lf.removedBlocks.add(cand, removedSet, other)
			case terminalStatus(status):
				lf.closedPrereq.add(cand, closed, other)
			}
		})
	if err != nil {
		return linkFacts{}, err
	}
	err = scan(`
		SELECT d.depends_on_id, d.bead_id, d.type, d.removed, d.created_at, d.removed_set_at, c.status, c.closed_at, c.created_at
		FROM dependencies d LEFT JOIN beads c ON c.id = d.bead_id AND d.type IN (?, ?) AND d.removed = 0
		WHERE d.depends_on_id IN (`+in+`)`, slices.Concat([]any{BlocksDepType, ParentChildDepType}, ids),
		func(cand, other, typ string, removed bool, _, status, closed, created sql.NullString) {
			if removed || !status.Valid {
				return
			}
			switch {
			case typ == ParentChildDepType && terminalStatus(status):
				lf.closedChildren.add(cand, closed, other)
			case typ == BlocksDepType && !terminalStatus(status) && created.Valid:
				lf.openDownstream.add(cand, sql.NullString{String: created.String + " " + other, Valid: true}, other)
			}
		})
	return lf, err
}

type derivation struct {
	Input      adjustInput
	Result     adjustResult
	ReadySince Stamp
	Progress   Stamp
	Demand     DemandEvidence
}

type DemandEvidence struct {
	Milestone, Blocks string
	Due               bool
	Redetected        string
}

func (e DemandEvidence) Any() bool {
	return e.Milestone != "" || e.Blocks != "" || e.Due || e.Redetected != ""
}

func TimeAdjust(ctx context.Context, q Querier, now string, coef Coefficients, raised map[string]EffectivePriority, candidates []Bead) (TimeAdjustment, error) {
	nowT, err := time.Parse(time.RFC3339Nano, now)
	if err != nil {
		return TimeAdjustment{}, fmt.Errorf("time adjust: now %q: %w", now, err)
	}

	out := TimeAdjustment{Eff: make(map[string]EffectivePriority, len(raised)), Demand: map[string]bool{}, Held: map[string]bool{}, HeldSince: map[string]time.Time{}, HeldFrom: map[string]time.Time{}}
	maps.Copy(out.Eff, raised)
	if len(candidates) == 0 {
		return out, nil
	}
	ds, err := derive(ctx, q, nowT, coef, raised, candidates, []any{StatusOpen})
	if err != nil {
		return TimeAdjustment{}, err
	}
	for _, b := range candidates {
		d := ds[b.ID]
		if d.Demand.Any() {
			out.Demand[b.ID] = true
		}
		if d.Result.Held {
			out.Held[b.ID] = true
			out.HeldSince[b.ID] = d.Progress.Time
			out.HeldFrom[b.ID] = d.Result.HeldFrom
		}
		if d.Result.Source != "" {
			out.Eff[b.ID] = EffectivePriority{Priority: d.Result.Priority, Source: d.Result.Source}
		}
	}
	return out, nil
}

func derive(ctx context.Context, q Querier, nowT time.Time, coef Coefficients, raised map[string]EffectivePriority, candidates []Bead, statuses []any) (map[string]derivation, error) {
	params := coef.adjustParams()
	window := coef.Duration("redetect_window")

	ids, in := candidateIDs(candidates)
	facts, err := beadTimesOf(ctx, q, ids, statuses)
	if err != nil {
		return nil, err
	}
	links, err := linkFactsOf(ctx, q, ids, in)
	if err != nil {
		return nil, err
	}
	closedPrereq, removedBlocks := links.closedPrereq.stamps("prereq"), links.removedBlocks.stamps("removed")
	closedChildren, linkTouched := links.closedChildren.stamps("child"), links.touched.stamps("dependency")
	demandSet, err := demandOf(ctx, q, nowT, window, candidates, facts, links.openDownstream.firsts())
	if err != nil {
		return nil, err
	}

	out := make(map[string]derivation, len(candidates))
	for _, b := range candidates {
		f := facts[b.ID]
		progress := latestStamp(f.progress, linkTouched[b.ID])
		readySince := latestStamp(f.readySince, closedPrereq[b.ID], removedBlocks[b.ID], closedChildren[b.ID])
		input := adjustInput{
			Graph:      effectiveOf(raised, b.ID, f.priority),
			Stored:     f.priority,
			Severity:   f.severity,
			Demand:     demandSet[b.ID].Any(),
			Gate:       f.typ == BeadTypeGate,
			ReadySince: readySince.Time,
			Progress:   progress.Time,
			Now:        nowT,
		}
		out[b.ID] = derivation{Input: input, Result: adjust(params, input), ReadySince: readySince, Progress: progress, Demand: demandSet[b.ID]}
	}
	return out, nil
}

func firstByCandidate(ctx context.Context, q Querier, query string, args ...any) (map[string]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("time adjust: query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var id, key string
		if err := rows.Scan(&id, &key); err != nil {
			return nil, fmt.Errorf("time adjust: scan: %w", err)
		}
		_, first, _ := strings.Cut(key, " ")
		out[id] = first
	}
	return out, rows.Err()
}

func candidateIDs(candidates []Bead) ([]any, string) {
	ids := make([]any, len(candidates))
	for i, b := range candidates {
		ids[i] = b.ID
	}
	return ids, inPlaceholders(len(ids))
}

func openDownstreamOf(ctx context.Context, q Querier, candidates []Bead) (map[string]string, error) {
	ids, in := candidateIDs(candidates)
	return firstByCandidate(ctx, q, `
		SELECT d.depends_on_id, MIN(b.created_at || ' ' || b.id) FROM dependencies d JOIN beads b ON b.id = d.bead_id
		WHERE d.type = ? AND d.removed = 0 AND b.status NOT IN (?, ?) AND d.depends_on_id IN (`+in+`) GROUP BY d.depends_on_id`,
		slices.Concat([]any{BlocksDepType, StatusClosed, StatusCancelled}, ids)...)
}

func demandOf(ctx context.Context, q Querier, nowT time.Time, window time.Duration, candidates []Bead, facts map[string]beadTimes, blocksOpenDownstream map[string]string) (map[string]DemandEvidence, error) {
	ids, in := candidateIDs(candidates)
	terminal := []any{StatusClosed, StatusCancelled}
	inMilestone, err := firstByCandidate(ctx, q, `
		WITH RECURSIVE anc(cand, id) AS (
			SELECT d.bead_id, d.depends_on_id FROM dependencies d
			WHERE d.type = ? AND d.removed = 0 AND d.bead_id IN (`+in+`)
			UNION
			SELECT anc.cand, d.depends_on_id FROM dependencies d JOIN anc ON d.bead_id = anc.id
			WHERE d.type = ? AND d.removed = 0
		) SELECT anc.cand, MIN(m.created_at || ' ' || m.id) FROM anc CROSS JOIN beads m ON m.id = anc.id
		WHERE m.status NOT IN (?, ?) AND m.labels LIKE '%"milestone:%' GROUP BY anc.cand`,
		slices.Concat([]any{ParentChildDepType}, ids, []any{ParentChildDepType}, terminal)...)
	if err != nil {
		return nil, err
	}
	out := map[string]DemandEvidence{}
	for _, b := range candidates {
		key := b.ID
		f := facts[key]
		ev := DemandEvidence{Milestone: inMilestone[key], Blocks: blocksOpenDownstream[key], Due: f.due != ""}
		if f.redetected != "" {
			if t, err := time.Parse(time.RFC3339Nano, f.redetected); err == nil && t.After(nowT.Add(-window)) {
				ev.Redetected = f.redetected
			}
		}
		if ev.Any() {
			out[key] = ev
		}
	}
	return out, nil
}

func CandidateDemand(ctx context.Context, q Querier, now string, coef Coefficients, candidates []Bead) (map[string]bool, error) {
	nowT, err := time.Parse(time.RFC3339Nano, now)
	if err != nil {
		return nil, fmt.Errorf("demand: now %q: %w", now, err)
	}
	if len(candidates) == 0 {
		return map[string]bool{}, nil
	}
	ids := make([]any, len(candidates))
	for i, b := range candidates {
		ids[i] = b.ID
	}
	rows, err := q.QueryContext(ctx, `
		SELECT id, due_at, last_redetected_at FROM beads WHERE id IN (`+inPlaceholders(len(ids))+`)`, ids...)
	if err != nil {
		return nil, fmt.Errorf("demand: query beads: %w", err)
	}
	defer func() { _ = rows.Close() }()
	facts := map[string]beadTimes{}
	for rows.Next() {
		var id string
		var due, redetected sql.NullString
		if err := rows.Scan(&id, &due, &redetected); err != nil {
			return nil, fmt.Errorf("demand: scan beads: %w", err)
		}
		facts[id] = beadTimes{due: due.String, redetected: redetected.String}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("demand: iterate beads: %w", err)
	}
	downstream, err := openDownstreamOf(ctx, q, candidates)
	if err != nil {
		return nil, err
	}
	ev, err := demandOf(ctx, q, nowT, coef.Duration("redetect_window"), candidates, facts, downstream)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(ev))
	for id := range ev {
		out[id] = true
	}
	return out, nil
}

func beadTimesOf(ctx context.Context, q Querier, ids []any, statuses []any) (map[string]beadTimes, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, type, severity, priority, created_at, due_at, last_redetected_at,
		       status_set_at, claimed_by_set_at, priority_set_at, title_set_at, description_set_at,
		       external_refs_set_at, due_at_set_at, expedite_until_set_at, severity_set_at, revived_at
		FROM beads WHERE status IN (`+inPlaceholders(len(statuses))+`) AND id IN (`+inPlaceholders(len(ids))+`)`,
		slices.Concat(statuses, ids)...)
	if err != nil {
		return nil, fmt.Errorf("time adjust: query beads: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]beadTimes{}
	for rows.Next() {
		var id string
		var f beadTimes
		var due, redetected sql.NullString
		var sets [10]sql.NullString
		if err := rows.Scan(&id, &f.typ, &f.severity, &f.priority, &f.created, &due, &redetected,
			&sets[0], &sets[1], &sets[2], &sets[3], &sets[4], &sets[5], &sets[6], &sets[7], &sets[8], &sets[9]); err != nil {
			return nil, fmt.Errorf("time adjust: scan beads: %w", err)
		}
		f.due, f.redetected = due.String, redetected.String
		created := parseStamp(sql.NullString{String: f.created, Valid: true})
		f.readySince = latestStamp(Stamp{Time: created, Kind: "status"}, Stamp{Time: parseStamp(sets[0]), Kind: "status"})
		stamps := []Stamp{{Time: created, Kind: "created_at"}}
		for i, name := range progressColumns {
			stamps = append(stamps, Stamp{Time: parseStamp(sets[i]), Kind: name})
		}
		f.progress = latestStamp(stamps...)
		out[id] = f
	}
	return out, rows.Err()
}

var progressColumns = [10]string{
	"status_set_at", "claimed_by_set_at", "priority_set_at", "title_set_at", "description_set_at",
	"external_refs_set_at", "due_at_set_at", "expedite_until_set_at", "severity_set_at", "revived_at",
}
