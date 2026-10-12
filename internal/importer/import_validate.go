// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"encoding/json"
	"fmt"

	"github.com/loombeading/loom/internal/domain"
)

var identScalarKeys = map[string][]string{
	"bead":       {"id", "claimed_by"},
	"dependency": {"bead_id", "depends_on_id"},
	"token_cost": {"id", "bead_id", "actor"},
}

var identListKeys = map[string][]string{
	"bead": {"labels", "external_refs"},
}

func identCharsError(lineNo int, typ string, row importRow) error {
	for _, key := range identScalarKeys[typ] {
		var s string
		if json.Unmarshal(row[key], &s) != nil {
			continue
		}
		if err := forbiddenIdentError(lineNo, typ, key, s); err != nil {
			return err
		}
	}
	for _, key := range identListKeys[typ] {
		var list []string
		if json.Unmarshal(row[key], &list) != nil {
			continue
		}
		for _, s := range list {
			if err := forbiddenIdentError(lineNo, typ, key, s); err != nil {
				return err
			}
		}
	}
	return nil
}

func forbiddenIdentError(lineNo int, typ, key, s string) error {
	r, bad := domain.ForbiddenIdentRune(s)
	if !bad {
		return nil
	}
	return fmt.Errorf("import: line %d: %s field %q contains forbidden character U+%04X", lineNo, typ, key, r)
}
