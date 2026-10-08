// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"sort"
	"strconv"

	"github.com/loombeading/loom/internal/domain"
)

const (
	colStatus         = "status"
	colBeadType       = "type"
	colReasoningDepth = "reasoning_depth"
)

type importBeadCol struct {
	jsonKey string
	col     string
}

var importBeadCols = []importBeadCol{
	{"namespace", "namespace"},
	{"title", "title"},
	{"description", "description"},
	{colStatus, colStatus},
	{"priority", "priority"},
	{colBeadType, colBeadType},
	{"labels", "labels"},
	{"claimed_by", "claimed_by"},
	{"claim_expires_at", "claim_expires_at"},
	{"summary", "summary"},
	{"external_refs", "external_refs"},
	{colReasoningDepth, colReasoningDepth},
	{"closed_at", "closed_at"},
	{"severity", "severity"},
	{"due_at", "due_at"},
	{"expedite_until", "expedite_until"},
	{"expedite_reason", "expedite_reason"},
	{"redetect_key", "redetect_key"},
	{"last_redetected_at", "last_redetected_at"},
	{"revived_at", "revived_at"},
}

var importKnownBeadKeys = func() map[string]bool {
	m := map[string]bool{"_type": true, "id": true, "created_at": true, "updated_at": true, "_set_at": true}
	for _, c := range importBeadCols {
		m[c.jsonKey] = true
	}
	return m
}()

func rawAtMap(row importRow) (map[string]json.RawMessage, error) {
	raw, present := row["_set_at"]
	if !present || raw == nil {
		return map[string]json.RawMessage{}, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("import: _set_at: %w", err)
	}
	return m, nil
}

type beadColValue struct {
	raw      any
	rawBytes []byte
	ts       string
}

type Options struct {
	Actor    string
	Now      string
	ShortLen int
}

type Summary struct {
	NewBeads            int
	NewDeps             int
	NewTokenCosts       int
	UpdatedBeads        int
	UpdatedCoefficients int
	SkippedUnknown      int

	CycleLines  []string
	ParentLines []string
	Dangling    int
	NewlyReady  []string

	depsChanged bool
}

type importRow = map[string]json.RawMessage

func Run(ctx context.Context, tx *sql.Tx, r io.Reader, opt Options) (*Summary, error) {
	var beadRows []importRow
	var depRows []importRow
	var tcRows []importRow
	var coefficientRows []importRow
	skipped := 0

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var row importRow
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, fmt.Errorf("import: invalid JSON line: %w", err)
		}
		typ, err := rawString(row["_type"])
		if err != nil {
			return nil, fmt.Errorf("import: invalid _type: %w", err)
		}
		if err := renamedKeyError(lineNo, typ, row); err != nil {
			return nil, err
		}
		switch typ {
		case "bead":
			beadRows = append(beadRows, row)
		case "dependency":
			depRows = append(depRows, row)
		case "token_cost":
			tcRows = append(tcRows, row)
		case "coefficient":
			coefficientRows = append(coefficientRows, row)
		default:
			skipped++
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("import: read: %w", err)
	}

	sum := &Summary{SkippedUnknown: skipped}

	before, err := domain.ReadyIDSet(ctx, tx)
	if err != nil {
		return nil, err
	}

	if err := importBeads(ctx, tx, beadRows, opt, sum); err != nil {
		return nil, err
	}
	if err := importDependencies(ctx, tx, depRows, opt, sum); err != nil {
		return nil, err
	}
	if err := importTokenCosts(ctx, tx, tcRows, opt, sum); err != nil {
		return nil, err
	}
	if err := importCoefficients(ctx, tx, coefficientRows, opt, sum); err != nil {
		return nil, err
	}

	if err := resolveCyclesAndParents(ctx, tx, opt, sum); err != nil {
		return nil, err
	}
	dangling, err := countDangling(ctx, tx)
	if err != nil {
		return nil, err
	}
	sum.Dangling = dangling
	if sum.Dangling > 0 && sum.depsChanged {
		if err := domain.Audit(ctx, tx, domain.AuditRecord{
			OccurredAt: opt.Now, Actor: opt.Actor,
			Kind: domain.AuditKindMerge, Field: "dangling", Origin: domain.OriginImport,
			NewValue: strconv.Itoa(sum.Dangling),
		}); err != nil {
			return nil, err
		}
	}

	after, err := domain.ReadyIDSet(ctx, tx)
	if err != nil {
		return nil, err
	}
	for id := range after {
		if !before[id] {
			sum.NewlyReady = append(sum.NewlyReady, id)
		}
	}
	sort.Strings(sum.NewlyReady)

	return sum, nil
}

func rawString(raw json.RawMessage) (string, error) {
	if raw == nil {
		return "", nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", err
	}
	return s, nil
}

func rawInt(raw json.RawMessage) (int64, error) {
	var n int64
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, err
	}
	return n, nil
}

func rawBool(raw json.RawMessage) (bool, error) {
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return false, err
	}
	return b, nil
}

func canonicalLabels(raw json.RawMessage) (string, []string, error) {
	return canonicalStringList(raw, domain.NormalizeLabels)
}

func canonicalExternalRefs(raw json.RawMessage) (string, []string, error) {
	return canonicalStringList(raw, domain.NormalizeExternalRefs)
}

func canonicalStringList(raw json.RawMessage, normalizeFn func([]string) []string) (string, []string, error) {
	if raw == nil {
		return "", nil, nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		return "", nil, err
	}
	list = normalizeFn(list)
	b, err := domain.EncodeStringList(list)
	if err != nil {
		return "", nil, err
	}
	return b, list, nil
}

var renamedRowKeys = map[string]map[string]string{
	"bead":        {"dedupe_key": "redetect_key", "last_seen_at": "last_redetected_at", "depth": "reasoning_depth"},
	"dependency":  {"removed_at": "removed_set_at"},
	"coefficient": {"value_at": "value_set_at"},
}

func renamedKeyError(lineNo int, typ string, row importRow) error {
	if typ == "policy" {
		return fmt.Errorf("import: line %d: _type %q was renamed to %q", lineNo, typ, "coefficient")
	}
	if _, ok := row["_at"]; ok {
		return fmt.Errorf("import: line %d: key %q was renamed to %q", lineNo, "_at", "_set_at")
	}
	for _, old := range slices.Sorted(maps.Keys(renamedRowKeys[typ])) {
		if _, ok := row[old]; ok {
			return fmt.Errorf("import: line %d: %s key %q was renamed to %q", lineNo, typ, old, renamedRowKeys[typ][old])
		}
	}
	return nil
}
