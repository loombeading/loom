// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ErrGateTerminalOnlyResolve = errors.New("a Gate can only be resolved with lm gate resolve")

const waitDatePrefix = "wait:date:"

type UpdateInput struct {
	ID      string
	Claim   bool
	Release bool
	Force   bool
	Reopen  bool
	Cancel  bool

	Title              string
	Description        string
	Priority           int
	Type               string
	Namespace          string
	AddExternalRefs    []string
	RemoveExternalRefs []string
	Summary            string
	AddLabels          []string
	RemoveLabels       []string

	Depth int

	Severity    *int
	Due         *time.Time
	ExpediteFor *time.Duration
	Revive      bool

	Clear []string

	ClaimTTL time.Duration
	Clock    time.Time

	Reason string
	Actor  string
	Now    string
}

var clearableColumns = map[string]bool{
	"description":     true,
	"summary":         true,
	"reasoning_depth": true,
}

func (in UpdateInput) hasTransition() bool {
	return in.Claim || in.Release || in.Reopen || in.Cancel || len(in.Clear) > 0
}

func (in UpdateInput) hasFieldEdit() bool {
	return in.Title != "" || in.Description != "" || in.Priority != -1 || in.Type != "" ||
		in.Namespace != "" || len(in.AddExternalRefs) > 0 || len(in.RemoveExternalRefs) > 0 ||
		in.Summary != "" || len(in.AddLabels) > 0 || len(in.RemoveLabels) > 0 ||
		in.Depth != 0 || in.Severity != nil || in.Due != nil || in.ExpediteFor != nil || in.Revive
}

type beadRow struct {
	status, claimedBy, beadType, title string
	labels, externalRefs, description  sql.NullString
	claimTTL                           sql.NullString
}

func loadBeadRow(ctx context.Context, tx *sql.Tx, id string) (beadRow, error) {
	var row beadRow
	var claimedByNS sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT status, claimed_by, labels, type, external_refs, description, title, claim_expires_at FROM beads WHERE id = ?`, id).
		Scan(&row.status, &claimedByNS, &row.labels, &row.beadType, &row.externalRefs, &row.description, &row.title, &row.claimTTL)
	if errors.Is(err, sql.ErrNoRows) {
		return row, ErrBeadNotFound
	}
	if err != nil {
		return row, fmt.Errorf("update bead: lookup: %w", err)
	}
	row.claimedBy = claimedByNS.String
	return row, nil
}

func UpdateBead(ctx context.Context, tx *sql.Tx, in UpdateInput) ([]string, error) {
	transitionCount := 0
	for _, b := range []bool{in.Claim, in.Release, in.Reopen, in.Cancel} {
		if b {
			transitionCount++
		}
	}
	if transitionCount > 1 {
		return nil, &ValidationError{Msg: "at most one of --claim, --release, --reopen, --status cancelled may be given"}
	}

	if in.ClaimTTL < 0 || (in.ClaimTTL != 0 && !in.Claim) {
		return nil, &ValidationError{Msg: "--claim-ttl must be positive and requires --claim"}
	}

	row, err := loadBeadRow(ctx, tx, in.ID)
	if err != nil {
		return nil, err
	}

	if !in.hasTransition() && !in.hasFieldEdit() {
		if in.Reason == "" {
			return nil, &ValidationError{Msg: "no changes and no --reason given"}
		}
		return nil, Audit(ctx, tx, AuditRecord{OccurredAt: in.Now, Actor: in.Actor, BeadID: in.ID, Kind: AuditKindNote, Reason: in.Reason})
	}

	sets := map[string]any{}
	var auditRecs []AuditRecord
	var warnings []string
	steps := []func() ([]AuditRecord, []string, error){
		func() ([]AuditRecord, []string, error) {
			recs, err := applyTransition(ctx, tx, in, row, sets)
			return recs, nil, err
		},
		func() ([]AuditRecord, []string, error) {
			recs, err := applyScalarFields(in, row.beadType, sets)
			return recs, nil, err
		},
		func() ([]AuditRecord, []string, error) {
			recs, err := applySchedule(ctx, tx, in, sets)
			return recs, nil, err
		},
		func() ([]AuditRecord, []string, error) {
			recs, err := applyClear(in.Clear, sets)
			return recs, nil, err
		},
		func() ([]AuditRecord, []string, error) { return applyLabels(ctx, tx, in, row, sets) },
		func() ([]AuditRecord, []string, error) {
			recs, err := applyExternalRefs(ctx, tx, in, row, sets)
			return recs, nil, err
		},
	}
	for _, step := range steps {
		recs, warns, err := step()
		if err != nil {
			return nil, err
		}
		auditRecs = append(auditRecs, recs...)
		warnings = append(warnings, warns...)
	}

	if err := execUpdate(ctx, tx, in.ID, sets, in.Now); err != nil {
		return nil, err
	}
	if in.Priority != -1 {
		if err := checkMilestoneOrder(ctx, tx, in.ID); err != nil {
			return nil, err
		}
	}
	for _, rec := range auditRecs {
		rec.OccurredAt, rec.Actor, rec.BeadID, rec.Reason = in.Now, in.Actor, in.ID, in.Reason
		if err := Audit(ctx, tx, rec); err != nil {
			return nil, err
		}
	}
	return warnings, nil
}

func applyTransition(ctx context.Context, tx *sql.Tx, in UpdateInput, row beadRow, sets map[string]any) ([]AuditRecord, error) {
	switch {
	case in.Claim:
		return applyClaim(ctx, tx, in, row, sets)
	case in.Release:
		return applyRelease(in, row, sets)
	case in.Reopen:
		return applyReopen(ctx, tx, in, row, sets)
	case in.Cancel:
		return applyCancel(ctx, tx, in, row, sets)
	}
	return nil, nil
}

func applyClaim(ctx context.Context, tx *sql.Tx, in UpdateInput, row beadRow, sets map[string]any) ([]AuditRecord, error) {
	if row.beadType == BeadTypeGate {
		return nil, &ValidationError{Msg: "gate Beads cannot be claimed; a Gate is a wait primitive, not something to work on (use `lm gate resolve` to unblock its waiters)"}
	}
	clock := in.Clock
	if clock.IsZero() {
		clock = time.Now().UTC()
	}
	renewal, takeover := false, false
	if row.status != StatusOpen {
		if row.status != StatusInProgress || row.claimedBy == "" {
			return nil, &ValidationError{Msg: fmt.Sprintf("cannot claim a Bead in status %q", row.status)}
		}
		renewal = row.claimedBy == in.Actor && in.ClaimTTL > 0
		if !renewal {
			var err error
			if takeover, err = claimExpired(row, in.Actor, clock); err != nil {
				return nil, err
			}
			if !takeover {
				return nil, &ValidationError{Msg: "already claimed by " + row.claimedBy}
			}
		}
	}
	blockers, err := openBlocksBlockers(ctx, tx, in.ID)
	if err != nil {
		return nil, err
	}
	field := FieldClaim
	if len(blockers) > 0 {
		if !in.Force {
			ids, err := displayIDsFor(ctx, tx, blockers)
			if err != nil {
				return nil, err
			}
			return nil, &ValidationError{Msg: "blocked by " + strings.Join(ids, ", ")}
		}
		if in.Reason == "" {
			return nil, &ValidationError{Msg: "--claim --force requires --reason"}
		}
		field = FieldForceClaim
	}
	if in.ClaimTTL > 0 {
		sets[colClaimExpiresAt] = clock.Add(in.ClaimTTL).UTC().Format(filterTimeFormat)
	} else {
		sets[colClaimExpiresAt] = nil
	}
	if renewal {
		return nil, nil
	}
	sets["claimed_by"] = in.Actor
	if !takeover {
		sets[colStatus] = StatusInProgress
		return []AuditRecord{{Kind: AuditKindField, Field: field, NewValue: in.Actor}}, nil
	}
	recs := []AuditRecord{{Kind: AuditKindField, Field: FieldClaimTakeover, NewValue: in.Actor}}
	if field == FieldForceClaim {
		recs = append(recs, AuditRecord{Kind: AuditKindField, Field: field, NewValue: in.Actor})
	}
	return recs, nil
}

func claimExpired(row beadRow, actor string, clock time.Time) (bool, error) {
	if row.claimedBy == actor || !row.claimTTL.Valid {
		return false, nil
	}
	expiry, err := time.Parse(filterTimeFormat, row.claimTTL.String)
	if err != nil {
		return false, fmt.Errorf("claim: parse claim_expires_at %q: %w", row.claimTTL.String, err)
	}
	return !expiry.After(clock), nil
}

func applyRelease(in UpdateInput, row beadRow, sets map[string]any) ([]AuditRecord, error) {
	if row.status != StatusInProgress {
		return nil, &ValidationError{Msg: fmt.Sprintf("cannot release a Bead in status %q", row.status)}
	}
	field := FieldRelease
	if row.claimedBy != in.Actor {
		if !in.Force {
			return nil, &ValidationError{Msg: fmt.Sprintf("claimed by %s; use --force to release", row.claimedBy)}
		}
		field = FieldForceRelease
	}
	sets[colStatus] = StatusOpen
	sets["claimed_by"] = nil
	sets[colClaimExpiresAt] = nil
	return []AuditRecord{{Kind: AuditKindField, Field: field, NewValue: in.Actor}}, nil
}

func applyReopen(ctx context.Context, tx *sql.Tx, in UpdateInput, row beadRow, sets map[string]any) ([]AuditRecord, error) {
	if row.beadType == BeadTypeGate {
		return nil, ErrGateTerminalOnlyResolve
	}
	if !IsTerminalStatus(row.status) {
		return nil, &ValidationError{Msg: fmt.Sprintf("cannot reopen a Bead in status %q", row.status)}
	}
	if err := checkReopenTarget(ctx, tx, in.ID); err != nil {
		return nil, err
	}
	sets[colStatus] = StatusOpen
	sets["claimed_by"] = nil
	sets[colClaimExpiresAt] = nil
	sets[colClosedAt] = nil
	return []AuditRecord{{Kind: AuditKindField, Field: FieldReopen, NewValue: StatusOpen}}, nil
}

func applyCancel(ctx context.Context, tx *sql.Tx, in UpdateInput, row beadRow, sets map[string]any) ([]AuditRecord, error) {
	if row.beadType == BeadTypeGate {
		return nil, ErrGateTerminalOnlyResolve
	}
	if !CanTransition(row.status, StatusCancelled) {
		return nil, &ValidationError{Msg: fmt.Sprintf("cannot cancel a Bead in status %q", row.status)}
	}
	if err := checkNoUnfinishedDependents(ctx, tx, []string{in.ID}); err != nil {
		return nil, err
	}
	sets[colStatus] = StatusCancelled
	sets[colClosedAt] = in.Now
	if row.status == StatusInProgress {
		sets[colClaimExpiresAt] = nil
	}
	return []AuditRecord{
		{Kind: AuditKindField, Field: colStatus, NewValue: StatusCancelled},
		{Kind: AuditKindField, Field: colClosedAt, NewValue: in.Now},
	}, nil
}

func applyScalarFields(in UpdateInput, beadType string, sets map[string]any) ([]AuditRecord, error) {
	var recs []AuditRecord
	set := func(col string, val any, audit string) {
		sets[col] = val
		recs = append(recs, AuditRecord{Kind: AuditKindField, Field: col, NewValue: audit})
	}
	if in.Title != "" {
		set("title", in.Title, in.Title)
	}
	if in.Description != "" {
		set("description", in.Description, in.Description)
	}
	if in.Priority != -1 {
		if in.Priority < MinPriority || in.Priority > MaxPriority {
			return nil, &ValidationError{Msg: fmt.Sprintf("priority must be between %d and %d", MinPriority, MaxPriority)}
		}
		set("priority", in.Priority, strconv.Itoa(in.Priority))
	}
	if in.Type != "" {
		if !ValidBeadTypes[in.Type] {
			return nil, &ValidationError{Msg: fmt.Sprintf("invalid type %q", in.Type)}
		}
		if (beadType == BeadTypeGate) != (in.Type == BeadTypeGate) {
			return nil, &ValidationError{Msg: fmt.Sprintf("cannot change type from %q to %q; a Gate is created with `lm gate create` and ended with `lm gate resolve`/`lm gate reject`", beadType, in.Type)}
		}
		set("type", in.Type, in.Type)
	}
	if in.Namespace != "" {
		if err := ValidateNamespace(in.Namespace); err != nil {
			return nil, &ValidationError{Msg: err.Error()}
		}
		set("namespace", in.Namespace, in.Namespace)
	}
	if in.Summary != "" {
		set("summary", in.Summary, in.Summary)
	}
	if in.Depth != 0 {
		if in.Depth < MinReasoningDepth || in.Depth > MaxReasoningDepth {
			return nil, &ValidationError{Msg: fmt.Sprintf("reasoning_depth must be between %d and %d", MinReasoningDepth, MaxReasoningDepth)}
		}
		set("reasoning_depth", in.Depth, strconv.Itoa(in.Depth))
	}
	return recs, nil
}

func applySchedule(ctx context.Context, tx *sql.Tx, in UpdateInput, sets map[string]any) ([]AuditRecord, error) {
	var recs []AuditRecord
	if in.Severity != nil {
		if err := validateSeverity(*in.Severity); err != nil {
			return nil, err
		}
		sets["severity"] = *in.Severity
		recs = append(recs, AuditRecord{Kind: AuditKindField, Field: "severity", NewValue: strconv.Itoa(*in.Severity)})
	}
	if in.Due != nil {
		due := FormatTime(*in.Due)
		sets["due_at"] = due
		recs = append(recs, AuditRecord{Kind: AuditKindField, Field: "due_at", NewValue: due})
	}
	if in.ExpediteFor != nil {
		until, err := checkExpedite(ctx, tx, in.ID, *in.ExpediteFor, in.Reason, clockOrNow(in.Clock))
		if err != nil {
			return nil, err
		}
		sets["expedite_until"] = until
		sets["expedite_reason"] = in.Reason
		recs = append(recs,
			AuditRecord{Kind: AuditKindField, Field: "expedite_until", NewValue: until},
			AuditRecord{Kind: AuditKindField, Field: "expedite_reason", NewValue: in.Reason})
	}
	if in.Revive {
		if in.Reason == "" {
			return nil, &ValidationError{Msg: "--revive requires --reason"}
		}
		sets["revived_at"] = in.Now
		recs = append(recs, AuditRecord{Kind: AuditKindField, Field: "revived_at", NewValue: in.Now})
	}
	return recs, nil
}

func applyClear(fields []string, sets map[string]any) ([]AuditRecord, error) {
	var recs []AuditRecord
	for _, f := range fields {
		switch f {
		case "due":
			sets["due_at"] = nil
			recs = append(recs, AuditRecord{Kind: AuditKindField, Field: "due_at", NewValue: ""})
			continue
		case "expedite":
			sets["expedite_until"] = nil
			sets["expedite_reason"] = nil
			recs = append(recs,
				AuditRecord{Kind: AuditKindField, Field: "expedite_until", NewValue: ""},
				AuditRecord{Kind: AuditKindField, Field: "expedite_reason", NewValue: ""})
			continue
		}
		if !clearableColumns[f] {
			return nil, &ValidationError{Msg: fmt.Sprintf("unknown --clear field %q", f)}
		}
		sets[f] = nil
		recs = append(recs, AuditRecord{Kind: AuditKindField, Field: f, NewValue: ""})
	}
	return recs, nil
}

func applyLabels(ctx context.Context, tx *sql.Tx, in UpdateInput, row beadRow, sets map[string]any) ([]AuditRecord, []string, error) {
	if len(in.AddLabels) == 0 && len(in.RemoveLabels) == 0 {
		return nil, nil, nil
	}
	if err := rejectReleaseLabel(in.AddLabels); err != nil {
		return nil, nil, err
	}
	current, err := decodeStringList(row.labels)
	if err != nil {
		return nil, nil, err
	}
	next := applyDelta(current, in.AddLabels, in.RemoveLabels, NormalizeLabels)
	var warnings []string
	if row.beadType == BeadTypeGate {
		var warning string
		if next, warning, err = checkGateLabels(ctx, tx, in, row, current, next); err != nil {
			return nil, nil, err
		}
		if warning != "" {
			warnings = append(warnings, warning)
		}
	}
	recs, err := setStringList(sets, "labels", next)
	return recs, warnings, err
}

func checkGateLabels(ctx context.Context, tx *sql.Tx, in UpdateInput, row beadRow, current, next []string) ([]string, string, error) {
	next, err := checkGateAdjudication(ctx, tx, current, next)
	if err != nil {
		return nil, "", err
	}
	desc := in.Description
	if desc == "" {
		desc = row.description.String
	}
	if err := checkGateMaterial(desc, current, next); err != nil {
		return nil, "", err
	}
	if !IsTerminalStatus(row.status) {
		if err := checkNewWaitTargets(ctx, tx, in.ID, current, next); err != nil {
			return nil, "", err
		}
	}
	return next, waitDateOnTimedGateWarning(in, row, desc), nil
}

var timeOfDayRE = regexp.MustCompile(`[0-9]{1,2}:[0-9]{2}`)

func waitDateOnTimedGateWarning(in UpdateInput, row beadRow, desc string) string {
	hasWaitDate := false
	for _, l := range in.AddLabels {
		if strings.HasPrefix(l, waitDatePrefix) {
			hasWaitDate = true
			break
		}
	}
	if !hasWaitDate {
		return ""
	}
	if !timeOfDayRE.MatchString(row.title) && !timeOfDayRE.MatchString(desc) {
		return ""
	}
	return "wait:date は日単位に丸める。窓の終わりの時刻を wait:after:<RFC3339> で付ける"
}

func applyExternalRefs(ctx context.Context, tx *sql.Tx, in UpdateInput, row beadRow, sets map[string]any) ([]AuditRecord, error) {
	if len(in.AddExternalRefs) == 0 && len(in.RemoveExternalRefs) == 0 {
		return nil, nil
	}
	current, err := decodeStringList(row.externalRefs)
	if err != nil {
		return nil, err
	}
	next := applyDelta(current, in.AddExternalRefs, in.RemoveExternalRefs, NormalizeExternalRefs)
	if row.beadType != BeadTypeGate {
		if err := checkNewOwnedRefs(ctx, tx, in.ID, row.status, current, next); err != nil {
			return nil, err
		}
	}
	return setStringList(sets, "external_refs", next)
}

func setStringList(sets map[string]any, col string, next []string) ([]AuditRecord, error) {
	encoded, err := EncodeStringList(next)
	if err != nil {
		return nil, err
	}
	if next == nil {
		sets[col] = nil
	} else {
		sets[col] = encoded
	}
	return []AuditRecord{{Kind: AuditKindField, Field: col, NewValue: encoded}}, nil
}

func applyDelta(current, add, remove []string, normalize func([]string) []string) []string {
	removeSet := make(map[string]bool, len(remove))
	for _, r := range remove {
		removeSet[r] = true
	}
	next := make([]string, 0, len(current)+len(add))
	for _, v := range current {
		if !removeSet[v] {
			next = append(next, v)
		}
	}
	next = append(next, add...)
	return normalize(next)
}

func execUpdate(ctx context.Context, tx *sql.Tx, id string, sets map[string]any, now string) error {
	if len(sets) == 0 {
		return nil
	}
	var query strings.Builder
	query.WriteString("UPDATE beads SET updated_at = ?")
	args := []any{now}
	for col, val := range sets {
		query.WriteString(", " + col + " = ?")
		args = append(args, val)
		if lwwColumns[col] {
			// #nosec G202
			query.WriteString(", " + col + "_set_at = ?")
			args = append(args, now)
		}
	}
	query.WriteString(" WHERE id = ?")
	args = append(args, id)

	if _, err := tx.ExecContext(ctx, query.String(), args...); err != nil {
		return fmt.Errorf("update bead: exec: %w", err)
	}
	return nil
}

var lwwColumns = map[string]bool{
	"namespace": true, "title": true, "description": true, colStatus: true,
	colClosedAt: true, "priority": true, "type": true,
	"claimed_by": true, "summary": true, "labels": true,
	"external_refs":   true,
	"reasoning_depth": true,
	colClaimExpiresAt: true,
	"severity":        true, "due_at": true, "expedite_until": true, "expedite_reason": true,
	"redetect_key": true, "last_redetected_at": true, "revived_at": true,
}

type PartialInput struct {
	SourceID string
	Title    string
	Actor    string
	Reason   string
	Now      string
}

func CreatePartial(ctx context.Context, tx *sql.Tx, in PartialInput) (string, error) {
	var namespace, beadType string
	var priority int
	err := tx.QueryRowContext(ctx, `SELECT namespace, type, priority FROM beads WHERE id = ?`, in.SourceID).
		Scan(&namespace, &beadType, &priority)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrBeadNotFound
	}
	if err != nil {
		return "", fmt.Errorf("create partial: lookup source: %w", err)
	}

	var parentID string
	err = tx.QueryRowContext(ctx, `
		SELECT depends_on_id FROM dependencies
		WHERE bead_id = ? AND type = ? AND removed = 0`,
		in.SourceID, ParentChildDepType,
	).Scan(&parentID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("create partial: lookup parent: %w", err)
	}

	newID, err := CreateBead(ctx, tx, CreateInput{
		Title:     in.Title,
		Priority:  priority,
		BeadType:  beadType,
		Namespace: namespace,
		Parent:    parentID,
		Actor:     in.Actor,
		Reason:    in.Reason,
		Now:       in.Now,
	})
	if err != nil {
		return "", err
	}

	if err := AddLink(ctx, tx, in.Actor, newID, in.SourceID, BlocksDepType, in.Now, in.Reason); err != nil {
		return "", err
	}
	if err := AddLink(ctx, tx, in.Actor, newID, in.SourceID, DiscoveredFromDepType, in.Now, in.Reason); err != nil {
		return "", err
	}

	return newID, nil
}

type CloseInput struct {
	ID     string
	Reason string
	Actor  string
	Now    string
}

func CloseBead(ctx context.Context, tx *sql.Tx, in CloseInput) error {
	status, beadType, err := lookupStatusAndType(ctx, tx, in.ID, "close bead")
	if err != nil {
		return err
	}
	if beadType == BeadTypeGate {
		return &ValidationError{Msg: "gate Beads cannot be closed directly; use `lm gate resolve` (P4)"}
	}
	if !CanTransition(status, StatusClosed) {
		return &ValidationError{Msg: fmt.Sprintf("cannot close a Bead in status %q", status)}
	}
	if err := checkNoUnfinishedChildren(ctx, tx, in.ID); err != nil {
		return err
	}

	if err := execUpdate(ctx, tx, in.ID, map[string]any{colStatus: StatusClosed, colClosedAt: in.Now}, in.Now); err != nil {
		return err
	}
	recs := []AuditRecord{
		{Kind: AuditKindField, Field: colStatus, NewValue: StatusClosed},
		{Kind: AuditKindField, Field: colClosedAt, NewValue: in.Now},
	}
	for _, rec := range recs {
		rec.OccurredAt, rec.Actor, rec.BeadID, rec.Reason = in.Now, in.Actor, in.ID, in.Reason
		if err := Audit(ctx, tx, rec); err != nil {
			return err
		}
	}
	return nil
}
