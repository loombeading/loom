// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"unicode"
)

var hostilePayloads = []string{
	"a\nb",
	"a\r\nb",
	"# heading",
	"- item",
	"> quote",
	"a|b",
	"a`b",
	"a\x1b[31mred",
	"a\x07b",
	"a\u0085b",
	"a\u009bb",
	"a\u202eb",
	"a\u200bb",
	"a\U000E0041b",
}

var callerEscapedFields = map[string]bool{
	"HistoryRow.Value":  true,
	"HistoryRow.Reason": true,
}

func fillStrings(t *testing.T, v any, payload string) {
	t.Helper()
	rv := reflect.ValueOf(v).Elem()
	rt := rv.Type()
	for i := range rt.NumField() {
		f := rv.Field(i)
		if callerEscapedFields[rt.Name()+"."+rt.Field(i).Name] {
			continue
		}
		if f.Kind() == reflect.String {
			f.SetString(payload)
		}
		if f.Kind() == reflect.Slice && f.Type().Elem().Kind() == reflect.String {
			f.Set(reflect.ValueOf([]string{payload, payload}))
		}
	}
}

func assertOneSafeLine(t *testing.T, name, payload, out string) {
	t.Helper()
	out = strings.TrimSuffix(out, "\n")
	if strings.ContainsAny(out, "\n\r") {
		t.Errorf("%s with %q: output spans lines: %q", name, payload, out)
	}
	for _, r := range out {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || (r >= 0xE0000 && r <= 0xE007F) {
			t.Errorf("%s with %q: output contains %U: %q", name, payload, r, out)
			return
		}
	}
	for i, r := range out {
		if (r == '|' || r == '`') && (i == 0 || out[i-1] != '\\') {
			t.Errorf("%s with %q: unescaped %q at %d: %q", name, payload, r, i, out)
			return
		}
	}
}

func TestEveryStringFieldIsEscaped(t *testing.T) {
	for _, p := range hostilePayloads {
		var row BeadRow
		fillStrings(t, &row, p)
		row.Type = "epic"
		assertOneSafeLine(t, "beadLine", p, beadLine(row))
		row.Type = p
		assertOneSafeLine(t, "beadLine(type)", p, beadLine(row))
		row.Alias = ""
		assertOneSafeLine(t, "beadLine(no alias)", p, beadLine(row))

		var buf bytes.Buffer
		WriteCreated(&buf, row)
		assertOneSafeLine(t, "WriteCreated", p, buf.String())

		var dep DepRow
		fillStrings(t, &dep, p)
		for _, dangling := range []bool{false, true} {
			dep.Dangling = dangling
			assertOneSafeLine(t, "linkLine", p, linkLine(dep))
			assertOneSafeLine(t, "derivedLine", p, derivedLine(dep))
		}

		var h HistoryRow
		fillStrings(t, &h, p)
		h.HasField, h.HasValue, h.HasReason = true, true, true
		h.Value, h.Reason = Escape(p), Escape(p)
		assertOneSafeLine(t, "historyLine(created)", p, historyLine(h))
		h.Created = ""
		assertOneSafeLine(t, "historyLine", p, historyLine(h))

		var c ChildStatusCount
		fillStrings(t, &c, p)
		c.Count = 1
		assertOneSafeLine(t, "ChildrenLine", p, ChildrenLine([]ChildStatusCount{c}))
	}
}
