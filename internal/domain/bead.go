// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ValidBeadTypes = map[string]bool{
	DefaultBeadType: true,
	BeadTypeGate:    true,
}

const (
	MinPriority     = 1
	MaxPriority     = 4
	DefaultPriority = 2
	DefaultBeadType = "task"
	BeadTypeGate    = "gate"
)

const (
	colStatus   = "status"
	colClosedAt = "closed_at"

	colClaimExpiresAt = "claim_expires_at"
)

const (
	MinReasoningDepth = 1
	MaxReasoningDepth = 5
)

type ValidationError struct {
	Msg string
}

func (e *ValidationError) Error() string { return e.Msg }

func dedupNonEmpty(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func NormalizeLabels(labels []string) []string {
	out := dedupNonEmpty(labels)
	sort.Strings(out)
	return out
}

func EncodeStringList(values []string) (string, error) {
	b, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("encode string list: %w", err)
	}
	return string(b), nil
}

func decodeStringList(raw sql.NullString) ([]string, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw.String), &values); err != nil {
		return nil, fmt.Errorf("decode string list: %w", err)
	}
	return values, nil
}

func NormalizeExternalRefs(refs []string) []string {
	return dedupNonEmpty(refs)
}

type CreateInput struct {
	Title          string
	Description    string
	Priority       int
	BeadType       string
	Labels         []string
	Namespace      string
	ExternalRefs   []string
	Summary        string
	Parent         string
	DiscoveredFrom string
	BlockedBy      []string

	Depth int

	Severity    int
	Due         *time.Time
	ExpediteFor *time.Duration
	Clock       time.Time
	RedetectKey string

	RequireMilestone bool

	ShortLen    int
	NoMilestone string

	Actor  string
	Reason string
	Now    string
}

const ErrMilestoneRequiredMsg = "lm ahead の Milestones を見て --parent を付けるか、--no-milestone に理由を書く"

const ErrParentUnaffiliatedMsg = "親が無所属。親を --parent でマイルストーンに所属させるか、--parent を外して --no-milestone に理由を書く"

const errDiscoveredFromUnaffiliatedMsg = "派生元が無所属。--parent を付けるか --no-milestone に理由を書く"

const ExemptMilestoneLabel = "exempt:milestone"

const AbolishedByLabelPrefix = "abolished-by:"

const (
	releaseLabelPrefix   = "release:"
	milestoneLabelPrefix = "milestone:"
)

func rejectReleaseLabel(labels []string) error {
	for _, l := range labels {
		name, ok := strings.CutPrefix(l, releaseLabelPrefix)
		if !ok {
			continue
		}
		return &ValidationError{Msg: fmt.Sprintf("%s%s ではなく %s%s を付ける（lm ahead は旧名の release: 接頭辞を読まない）", releaseLabelPrefix, name, milestoneLabelPrefix, name)}
	}
	return nil
}

func validateCreateInput(in CreateInput) error {
	if in.Severity != 0 {
		if err := validateSeverity(in.Severity); err != nil {
			return err
		}
	}
	if in.Title == "" {
		return &ValidationError{Msg: "title must not be empty"}
	}
	if in.Priority < MinPriority || in.Priority > MaxPriority {
		return &ValidationError{Msg: fmt.Sprintf("priority must be between %d and %d", MinPriority, MaxPriority)}
	}
	if !ValidBeadTypes[in.BeadType] {
		return &ValidationError{Msg: fmt.Sprintf("invalid type %q", in.BeadType)}
	}
	if err := ValidateNamespace(in.Namespace); err != nil {
		return &ValidationError{Msg: err.Error()}
	}
	if in.Depth != 0 && (in.Depth < MinReasoningDepth || in.Depth > MaxReasoningDepth) {
		return &ValidationError{Msg: fmt.Sprintf("reasoning_depth must be between %d and %d", MinReasoningDepth, MaxReasoningDepth)}
	}
	if in.RequireMilestone && in.BeadType != BeadTypeGate && in.Parent == "" && in.DiscoveredFrom == "" && in.NoMilestone == "" {
		return &ValidationError{Msg: ErrMilestoneRequiredMsg}
	}
	if err := rejectReleaseLabel(in.Labels); err != nil {
		return err
	}
	return nil
}

func checkCreateAffiliated(ctx context.Context, tx *sql.Tx, in CreateInput, parentID, discoveredFromID string) error {
	if parentID != "" {
		return checkAffiliated(ctx, tx, in, parentID, ErrParentUnaffiliatedMsg)
	}
	return checkAffiliated(ctx, tx, in, discoveredFromID, errDiscoveredFromUnaffiliatedMsg)
}

func checkAffiliated(ctx context.Context, tx *sql.Tx, in CreateInput, rootID, msg string) error {
	if !in.RequireMilestone || in.BeadType == BeadTypeGate || rootID == "" {
		return nil
	}
	affiliated, err := ancestorHasMilestoneLabel(ctx, tx, rootID, true)
	if err != nil {
		return err
	}
	if affiliated {
		return nil
	}
	return &ValidationError{Msg: msg}
}

func ancestorHasMilestoneLabel(ctx context.Context, tx *sql.Tx, rootID string, includeExempt bool) (bool, error) {
	exempt := ""
	if includeExempt {
		exempt = ExemptMilestoneLabel
	}
	var found bool
	err := tx.QueryRowContext(ctx, `
		WITH RECURSIVE ancestors(id) AS (
			SELECT ?
			UNION
			SELECT d.depends_on_id FROM dependencies d
			JOIN ancestors a ON d.bead_id = a.id
			WHERE d.type = ? AND d.removed = 0
		)
		SELECT EXISTS (
			SELECT 1 FROM beads b JOIN ancestors a ON b.id = a.id
			WHERE (? <> '' AND b.labels LIKE '%"' || ? || '"%') OR b.labels LIKE '%"milestone:%'
		)`, rootID, ParentChildDepType, exempt, exempt).Scan(&found)
	return found, err
}

func applyDiscoveredFromAffiliation(ctx context.Context, tx *sql.Tx, targets *createLinkTargets) (bool, error) {
	if targets.parent != "" || targets.discoveredFrom == "" {
		return false, nil
	}
	affiliated, err := ancestorHasMilestoneLabel(ctx, tx, targets.discoveredFrom, false)
	if err != nil || !affiliated {
		return !affiliated, err
	}
	targets.parent, err = discoveredFromParent(ctx, tx, targets.discoveredFrom)
	return false, err
}

func discoveredFromParent(ctx context.Context, tx *sql.Tx, sourceID string) (string, error) {
	var parent sql.NullString
	err := tx.QueryRowContext(ctx, `
		SELECT CASE WHEN b.labels LIKE '%"milestone:%' THEN b.id ELSE (
			SELECT d.depends_on_id FROM dependencies d
			WHERE d.bead_id = b.id AND d.type = ? AND d.removed = 0
		) END
		FROM beads b WHERE b.id = ?`, ParentChildDepType, sourceID).Scan(&parent)
	if err != nil {
		return "", fmt.Errorf("create bead: discovered-from parent: %w", err)
	}
	return parent.String, nil
}

func discoveredFromUnaffiliatedReason(sourceID string) string {
	return "discovered-from " + sourceID + ": 派生元がマイルストーンに属さない"
}

type createLinkTargets struct {
	parent         string
	discoveredFrom string
	blockedBy      []string
}

func resolveOptionalID(ctx context.Context, tx *sql.Tx, s string) (string, error) {
	if s == "" {
		return "", nil
	}
	return ResolveID(ctx, tx, s)
}

func resolveCreateLinkTargets(ctx context.Context, tx *sql.Tx, in CreateInput) (createLinkTargets, error) {
	var t createLinkTargets
	var err error
	if t.parent, err = resolveOptionalID(ctx, tx, in.Parent); err != nil {
		return t, err
	}
	if t.discoveredFrom, err = resolveOptionalID(ctx, tx, in.DiscoveredFrom); err != nil {
		return t, err
	}
	t.blockedBy = make([]string, 0, len(in.BlockedBy))
	for _, s := range in.BlockedBy {
		id, err := ResolveID(ctx, tx, s)
		if err != nil {
			return t, err
		}
		t.blockedBy = append(t.blockedBy, id)
	}
	return t, checkBlockersNotTerminal(ctx, tx, t.blockedBy)
}

func CreateBead(ctx context.Context, tx *sql.Tx, in CreateInput) (string, error) {
	if err := validateCreateInput(in); err != nil {
		return "", err
	}
	targets, err := resolveCreateLinkTargets(ctx, tx, in)
	if err != nil {
		return "", err
	}

	if err := checkCreateAffiliated(ctx, tx, in, targets.parent, targets.discoveredFrom); err != nil {
		return "", err
	}
	inherit, err := applyDiscoveredFromAffiliation(ctx, tx, &targets)
	if err != nil {
		return "", err
	}

	id, shortID, err := allocateCanonicalID(ctx, tx, in.ShortLen)
	if err != nil {
		return "", err
	}
	now := in.Now

	var descVal, labelsVal, externalRefsVal, summaryVal sql.NullString
	if in.Description != "" {
		descVal = sql.NullString{String: in.Description, Valid: true}
	}
	if in.Summary != "" {
		summaryVal = sql.NullString{String: in.Summary, Valid: true}
	}
	labels := in.Labels
	if in.NoMilestone != "" || inherit {
		labels = append(slices.Clone(labels), ExemptMilestoneLabel)
	}
	labels = NormalizeLabels(labels)
	if in.BeadType == BeadTypeGate {
		if labels, err = checkGateAdjudication(ctx, tx, nil, labels); err != nil {
			return "", err
		}
		if err := checkGateMaterial(in.Description, nil, labels); err != nil {
			return "", err
		}
	}
	if labels != nil {
		encoded, err := EncodeStringList(labels)
		if err != nil {
			return "", err
		}
		labelsVal = sql.NullString{String: encoded, Valid: true}
	}
	externalRefs := NormalizeExternalRefs(in.ExternalRefs)
	if externalRefs != nil {
		encoded, err := EncodeStringList(externalRefs)
		if err != nil {
			return "", err
		}
		externalRefsVal = sql.NullString{String: encoded, Valid: true}
	}
	var depthVal sql.NullInt64
	if in.Depth != 0 {
		depthVal = sql.NullInt64{Int64: int64(in.Depth), Valid: true}
	}
	severity := in.Severity
	if severity == 0 {
		severity = DefaultSeverity
	}
	var dueVal, expUntilVal, expReasonVal, redetectVal sql.NullString
	if in.RedetectKey != "" {
		redetectVal = sql.NullString{String: in.RedetectKey, Valid: true}
	}
	if in.Due != nil {
		dueVal = sql.NullString{String: FormatTime(*in.Due), Valid: true}
	}
	if in.ExpediteFor != nil {
		until, err := checkExpedite(ctx, tx, "", *in.ExpediteFor, in.Reason, clockOrNow(in.Clock))
		if err != nil {
			return "", err
		}
		expUntilVal = sql.NullString{String: until, Valid: true}
		expReasonVal = sql.NullString{String: in.Reason, Valid: true}
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO beads (
			short_id,
			id, namespace, namespace_set_at,
			title, title_set_at,
			description, description_set_at,
			status, status_set_at,
			priority, priority_set_at,
			type, type_set_at,
			created_at, updated_at,
			labels, labels_set_at,
			external_refs, external_refs_set_at,
			summary, summary_set_at,
			reasoning_depth, reasoning_depth_set_at,
			severity, severity_set_at,
			due_at, due_at_set_at,
			expedite_until, expedite_until_set_at,
			expedite_reason, expedite_reason_set_at,
			redetect_key, redetect_key_set_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		shortID,
		id, in.Namespace, now,
		in.Title, now,
		descVal,
		nullableTS(descVal, now),
		StatusOpen, now,
		in.Priority, now,
		in.BeadType, now,
		now, now,
		labelsVal,
		nullableTS(labelsVal, now),
		externalRefsVal,
		nullableTS(externalRefsVal, now),
		summaryVal,
		nullableTS(summaryVal, now),
		depthVal,
		nullableTSValid(depthVal.Valid, now),
		severity, now,
		dueVal, nullableTS(dueVal, now),
		expUntilVal, nullableTS(expUntilVal, now),
		expReasonVal, nullableTS(expReasonVal, now),
		redetectVal, nullableTS(redetectVal, now),
	)
	if err != nil {
		return "", fmt.Errorf("create bead: insert bead: %w", err)
	}
	var extra []AuditRecord
	if severity != DefaultSeverity {
		extra = append(extra, AuditRecord{Field: "severity", NewValue: strconv.Itoa(severity)})
	}
	if dueVal.Valid {
		extra = append(extra, AuditRecord{Field: "due_at", NewValue: dueVal.String})
	}
	if expUntilVal.Valid {
		extra = append(extra, AuditRecord{Field: "expedite_until", NewValue: expUntilVal.String}, AuditRecord{Field: "expedite_reason", NewValue: expReasonVal.String})
	}
	if redetectVal.Valid {
		extra = append(extra, AuditRecord{Field: "redetect_key", NewValue: redetectVal.String})
	}
	var inheritReason string
	if inherit {
		inheritReason = discoveredFromUnaffiliatedReason(targets.discoveredFrom)
	}
	if err := auditCreatedBead(ctx, tx, id, in, descVal, labelsVal, externalRefsVal, summaryVal, depthVal, extra, inheritReason); err != nil {
		return "", err
	}
	if err := addCreateLinks(ctx, tx, in, id, targets); err != nil {
		return "", err
	}
	if targets.parent != "" {
		if err := checkMilestoneOrder(ctx, tx, id); err != nil {
			return "", err
		}
	}
	return id, nil
}

func RedetectKey(ctx context.Context, tx *sql.Tx, namespace, key, actor, reason, now string) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM beads WHERE namespace = ? AND redetect_key = ? AND status IN (?, ?)`,
		namespace, key, StatusOpen, StatusInProgress).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("create bead: dedupe lookup: %w", err)
	}
	if err := execUpdate(ctx, tx, id, map[string]any{"last_redetected_at": now}, now); err != nil {
		return "", err
	}
	return id, Audit(ctx, tx, AuditRecord{OccurredAt: now, Actor: actor, BeadID: id, Kind: AuditKindField, Field: "last_redetected_at", NewValue: now, Reason: reason})
}

func auditCreatedBead(ctx context.Context, tx *sql.Tx, id string, in CreateInput, descVal, labelsVal, externalRefsVal, summaryVal sql.NullString, depthVal sql.NullInt64, extra []AuditRecord, inheritReason string) error {
	now := in.Now
	fields := []AuditRecord{
		{Field: "namespace", NewValue: in.Namespace},
		{Field: "title", NewValue: in.Title},
		{Field: colStatus, NewValue: StatusOpen},
		{Field: "priority", NewValue: strconv.Itoa(in.Priority)},
		{Field: "type", NewValue: in.BeadType},
	}
	if descVal.Valid {
		fields = append(fields, AuditRecord{Field: "description", NewValue: descVal.String})
	}
	if labelsVal.Valid {
		fields = append(fields, AuditRecord{Field: "labels", NewValue: labelsVal.String})
	}
	if externalRefsVal.Valid {
		fields = append(fields, AuditRecord{Field: "external_refs", NewValue: externalRefsVal.String})
	}
	if summaryVal.Valid {
		fields = append(fields, AuditRecord{Field: "summary", NewValue: summaryVal.String})
	}
	if depthVal.Valid {
		fields = append(fields, AuditRecord{Field: "reasoning_depth", NewValue: strconv.Itoa(in.Depth)})
	}
	fields = append(fields, extra...)
	for _, f := range fields {
		f.OccurredAt, f.Actor, f.BeadID, f.Kind = now, in.Actor, id, AuditKindField
		if err := Audit(ctx, tx, f); err != nil {
			return err
		}
	}
	for _, reason := range []string{in.Reason, noMilestoneReason(in.NoMilestone), inheritReason} {
		if reason == "" {
			continue
		}
		if err := Audit(ctx, tx, AuditRecord{OccurredAt: now, Actor: in.Actor, BeadID: id, Kind: AuditKindNote, Reason: reason}); err != nil {
			return err
		}
	}
	return nil
}

func noMilestoneReason(reason string) string {
	if reason == "" {
		return ""
	}
	return "no-milestone: " + reason
}

func addCreateLinks(ctx context.Context, tx *sql.Tx, in CreateInput, id string, targets createLinkTargets) error {
	if targets.parent != "" {
		if err := AddLink(ctx, tx, in.Actor, id, targets.parent, ParentChildDepType, in.Now, ""); err != nil {
			return err
		}
	}
	if targets.discoveredFrom != "" {
		if err := AddLink(ctx, tx, in.Actor, id, targets.discoveredFrom, DiscoveredFromDepType, in.Now, ""); err != nil {
			return err
		}
	}
	for _, blockerID := range targets.blockedBy {
		if err := AddLink(ctx, tx, in.Actor, id, blockerID, BlocksDepType, in.Now, ""); err != nil {
			return err
		}
	}
	return nil
}

func nullableTS(v sql.NullString, now string) any {
	return nullableTSValid(v.Valid, now)
}

func nullableTSValid(valid bool, now string) any {
	if !valid {
		return nil
	}
	return now
}

type Bead struct {
	ID           string
	Namespace    string
	Title        string
	Description  sql.NullString
	Status       string
	Priority     int
	BeadType     string
	ClaimedBy    sql.NullString
	Labels       []string
	ExternalRefs []string
	Summary      sql.NullString
	CreatedAt    string
	UpdatedAt    string
	ClosedAt     sql.NullString

	Depth sql.NullInt64

	ClaimExpiresAt sql.NullString

	Severity         int
	DueAt            sql.NullString
	ExpediteUntil    sql.NullString
	ExpediteReason   sql.NullString
	RedetectKey      sql.NullString
	LastRedetectedAt sql.NullString
	RevivedAt        sql.NullString
}

var ErrBeadNotFound = errors.New("bead not found")

func lookupStatusAndType(ctx context.Context, tx *sql.Tx, id, errPrefix string) (string, string, error) {
	var status, beadType string
	err := tx.QueryRowContext(ctx, `SELECT status, type FROM beads WHERE id = ?`, id).Scan(&status, &beadType)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrBeadNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("%s: lookup: %w", errPrefix, err)
	}
	return status, beadType, nil
}

func GetBead(ctx context.Context, q Querier, id string) (Bead, error) {
	var b Bead
	var labels, externalRefs sql.NullString
	err := q.QueryRowContext(ctx, `
		SELECT id, namespace, title, description, status, priority, type,
		       claimed_by, labels, external_refs, summary, created_at, updated_at, closed_at,
		       reasoning_depth, claim_expires_at, severity, due_at, expedite_until, expedite_reason,
		       redetect_key, last_redetected_at, revived_at
		FROM beads WHERE id = ?`, id,
	).Scan(&b.ID, &b.Namespace, &b.Title, &b.Description, &b.Status, &b.Priority, &b.BeadType,
		&b.ClaimedBy, &labels, &externalRefs, &b.Summary, &b.CreatedAt, &b.UpdatedAt, &b.ClosedAt,
		&b.Depth, &b.ClaimExpiresAt, &b.Severity, &b.DueAt, &b.ExpediteUntil, &b.ExpediteReason,
		&b.RedetectKey, &b.LastRedetectedAt, &b.RevivedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Bead{}, ErrBeadNotFound
	}
	if err != nil {
		return Bead{}, fmt.Errorf("get bead: %w", err)
	}
	b.ExternalRefs, err = decodeStringList(externalRefs)
	if err != nil {
		return Bead{}, err
	}
	b.Labels, err = decodeStringList(labels)
	if err != nil {
		return Bead{}, err
	}
	return b, nil
}

type DepRow struct {
	DependsOnID string
	Type        string
	Removed     bool
	CreatedAt   string
}

func Links(ctx context.Context, q Querier, id string, includeRemoved bool) ([]DepRow, error) {
	query := `SELECT depends_on_id, type, removed, created_at FROM dependencies WHERE bead_id = ?`
	if !includeRemoved {
		query += ` AND removed = 0`
	}
	query += ` ORDER BY created_at ASC, depends_on_id ASC`
	return queryDepRows(ctx, q, query, id)
}

func SentLinks(ctx context.Context, q Querier, id string, includeRemoved bool) ([]DepRow, error) {
	query := `SELECT bead_id, type, removed, created_at FROM dependencies WHERE depends_on_id = ?`
	if !includeRemoved {
		query += ` AND removed = 0`
	}
	query += ` ORDER BY created_at ASC, bead_id ASC`
	return queryDepRows(ctx, q, query, id)
}

func queryDepRows(ctx context.Context, q Querier, query, id string) ([]DepRow, error) {
	rows, err := q.QueryContext(ctx, query, id)
	if err != nil {
		return nil, fmt.Errorf("links: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []DepRow
	for rows.Next() {
		var r DepRow
		var removed bool
		if err := rows.Scan(&r.DependsOnID, &r.Type, &removed, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("links: scan: %w", err)
		}
		r.Removed = removed
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("links: iterate: %w", err)
	}
	return out, nil
}

type AuditRow struct {
	OccurredAt string
	Actor      string
	Kind       string
	Field      sql.NullString
	NewValue   sql.NullString
	Reason     sql.NullString
	Origin     string
}

func AuditHistory(ctx context.Context, q Querier, id string) ([]AuditRow, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT occurred_at, actor, kind, field, new_value, reason, origin
		FROM audit_log WHERE bead_id = ?
		ORDER BY occurred_at ASC, id ASC`, id)
	if err != nil {
		return nil, fmt.Errorf("audit history: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []AuditRow
	for rows.Next() {
		var r AuditRow
		if err := rows.Scan(&r.OccurredAt, &r.Actor, &r.Kind, &r.Field, &r.NewValue, &r.Reason, &r.Origin); err != nil {
			return nil, fmt.Errorf("audit history: scan: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit history: iterate: %w", err)
	}
	return out, nil
}

type ListFilter struct {
	All       bool
	Status    string
	Type      string
	Priority  int
	ClaimedBy string
	Label     string
	Namespace string

	ParentID string

	UpdatedBefore string
	UpdatedAfter  string

	ClaimExpiredAt string

	Limit int

	Sort string

	Now string
}

const (
	SortPriority = "priority"
	SortUpdated  = "updated"
)

const directChildOfClause = `
	EXISTS (
		SELECT 1 FROM dependencies pc
		WHERE pc.bead_id = beads.id AND pc.depends_on_id = ? AND pc.type = ? AND pc.removed = 0
	)`

func commonListFilterConds(query string, args []any, namespace, parentID, parentClause, updatedBefore, updatedAfter string) (string, []any) {
	if namespace != "" {
		query += ` AND namespace = ?`
		args = append(args, namespace)
	}
	if parentID != "" {
		query += ` AND ` + parentClause
		args = append(args, parentID, ParentChildDepType)
	}
	if updatedBefore != "" {
		query += ` AND updated_at < ?`
		args = append(args, updatedBefore)
	}
	if updatedAfter != "" {
		query += ` AND updated_at > ?`
		args = append(args, updatedAfter)
	}
	return query, args
}

func ListBeads(ctx context.Context, q Querier, f ListFilter) ([]Bead, int, error) {
	fromWhere, args := ListWhere(f)
	return runListQuery(ctx, q, fromWhere, args, f.Label, f.Limit, f.Sort)
}

func ListBeadsEffective(ctx context.Context, q Querier, f ListFilter) ([]Bead, map[string]EffectivePriority, int, error) {
	if f.Sort == SortUpdated {
		beads, total, err := ListBeads(ctx, q, f)
		return beads, nil, total, err
	}
	raised, err := RaisedPriorities(ctx, q, nowOrDefault(f.Now))
	if err != nil {
		return nil, nil, 0, err
	}
	fromWhere, args := ListWhere(f)
	if len(raised) == 0 {
		beads, total, err := runListQuery(ctx, q, fromWhere, args, f.Label, f.Limit, SortPriority)
		return beads, raised, total, err
	}
	beads, total, err := runListQuery(ctx, q, fromWhere, args, f.Label, 0, SortPriority)
	if err != nil {
		return nil, nil, 0, err
	}
	slices.SortFunc(beads, func(a, b Bead) int { return compareEffective(raised, a, b) })
	if f.Limit > 0 && f.Limit < total {
		beads = beads[:f.Limit]
	}
	return beads, raised, total, nil
}

func ListWhere(f ListFilter) (string, []any) {
	fromWhere := `FROM beads WHERE 1=1`
	var args []any
	statusIsTerminal := f.Status == StatusClosed || f.Status == StatusCancelled
	if !f.All && !statusIsTerminal {
		fromWhere += ` AND status NOT IN (?, ?)`
		args = append(args, StatusClosed, StatusCancelled)
	}
	if f.Status != "" {
		fromWhere += ` AND status = ?`
		args = append(args, f.Status)
	}
	if f.Type != "" {
		fromWhere += ` AND type = ?`
		args = append(args, f.Type)
	}
	if f.Priority >= 0 {
		fromWhere += ` AND priority = ?`
		args = append(args, f.Priority)
	}
	if f.ClaimedBy != "" {
		fromWhere += ` AND claimed_by = ?`
		args = append(args, f.ClaimedBy)
	}
	if f.ClaimExpiredAt != "" {
		fromWhere += ` AND status = ? AND claim_expires_at IS NOT NULL AND claim_expires_at <= ?`
		args = append(args, StatusInProgress, f.ClaimExpiredAt)
	}
	fromWhere, args = commonListFilterConds(fromWhere, args, f.Namespace, f.ParentID, directChildOfClause, f.UpdatedBefore, f.UpdatedAfter)
	return fromWhere, args
}

func ClaimClockString(env func(string) string) (string, error) {
	t, err := Now(env)
	if err != nil {
		return "", err
	}
	return t.Format(filterTimeFormat), nil
}

const listRowCols = `id, namespace, title, description, status, priority, type, claimed_by, labels, created_at, updated_at, closed_at, reasoning_depth, external_refs`

const listOrderClause = ` ORDER BY priority ASC, created_at ASC, id ASC`

const listOrderClauseUpdated = ` ORDER BY updated_at DESC, id ASC`

func listOrderClauseFor(sort string) string {
	if sort == SortUpdated {
		return listOrderClauseUpdated
	}
	return listOrderClause
}

func runListQuery(ctx context.Context, q Querier, fromWhere string, args []any, label string, limit int, sort string) ([]Bead, int, error) {
	orderClause := listOrderClauseFor(sort)
	if label == "" && limit > 0 {
		rowsQuery := "SELECT " + listRowCols + " " + fromWhere + orderClause + " LIMIT ?"
		rowArgs := append(append([]any{}, args...), limit)
		beads, err := queryBeads(ctx, q, rowsQuery, rowArgs, "")
		if err != nil {
			return nil, 0, err
		}
		var total int
		if err := q.QueryRowContext(ctx, "SELECT COUNT(*) "+fromWhere, args...).Scan(&total); err != nil {
			return nil, 0, fmt.Errorf("list beads: count: %w", err)
		}
		return beads, total, nil
	}

	rowsQuery := "SELECT " + listRowCols + " " + fromWhere + orderClause
	beads, err := queryBeads(ctx, q, rowsQuery, args, label)
	if err != nil {
		return nil, 0, err
	}
	total := len(beads)
	if limit > 0 && limit < total {
		beads = beads[:limit]
	}
	return beads, total, nil
}
