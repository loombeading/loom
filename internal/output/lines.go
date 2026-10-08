// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package output

import (
	"fmt"
	"io"
	"strings"
)

const DefaultLimit = 50

const (
	FilterLineFormat = "Filter: %s"

	TruncatedLineFormat = "Truncated: showing %d of %d"

	HistoryTruncatedLine = "Truncated: history values cut at 80 characters; use `lm show --full` to see them in full"

	NewlyReadyHeading = "## Newly ready"

	NewlyBlockedHeading = "## Newly blocked"

	CancelledHeading = "## Cancelled"

	RejectedLineFormat = "Rejected: %s (%s)"

	ClaimedLineFormat = "Claimed: %s"

	ErrorLineFormat = "Error: %s"
	noneLine        = "(none)"

	PickupNextLine      = "Pickup: ahead=0 next"
	PickupEtaFormat     = "Pickup: ahead=%d eta=%s〜%s"
	PickupOverrunFormat = "Pickup: ahead=%d eta=不定(割り込み超過)"
	PickupDivergesLabel = "不定"
	WaitingLineFormat   = "Waiting: %s"
)

type Cond struct {
	Key   string
	Value string
}

func WriteFilter(w io.Writer, conds []Cond) {
	if len(conds) == 0 {
		return
	}
	parts := make([]string, len(conds))
	for i, c := range conds {
		parts[i] = fmt.Sprintf(`%s="%s"`, c.Key, escapeInline(c.Value))
	}
	fmt.Fprintf(w, FilterLineFormat+"\n", strings.Join(parts, ", "))
}

func WriteTruncated(w io.Writer, shown, total int) {
	if shown == total {
		return
	}
	fmt.Fprintf(w, TruncatedLineFormat+"\n", shown, total)
}

func WriteNewlyReady(w io.Writer, conds []Cond, rows []BeadRow) {
	fmt.Fprintln(w, NewlyReadyHeading)
	WriteFilter(w, conds)
	WriteBeadList(w, rows)
}

func WriteNewlyBlocked(w io.Writer, conds []Cond, rows []BeadRow) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintln(w, "\n"+NewlyBlockedHeading)
	WriteFilter(w, conds)
	WriteBeadList(w, rows)
}

func WriteCancelled(w io.Writer, rows []BeadRow) {
	fmt.Fprintln(w, CancelledHeading)
	WriteBeadList(w, rows)
}

func WriteRejected(w io.Writer, id, reason string) {
	fmt.Fprintf(w, RejectedLineFormat+"\n", escapeInline(id), escapeInline(reason))
}

func WriteClaimed(w io.Writer, id string) {
	fmt.Fprintf(w, ClaimedLineFormat+"\n", escapeInline(id))
}

func WriteError(w io.Writer, msg string) {
	fmt.Fprintf(w, ErrorLineFormat+"\n", escapeInline(msg))
}

func WritePickup(w io.Writer, line string) {
	if line == "" {
		return
	}
	fmt.Fprintln(w, line)
	fmt.Fprintln(w)
}
