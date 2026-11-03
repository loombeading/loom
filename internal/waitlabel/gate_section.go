// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package waitlabel

import (
	"strings"

	"github.com/loombeading/loom/internal/domain"
)

const (
	GateSectionHuman      = "human"
	GateSectionPrereq     = "prereq"
	GateSectionAdjudicate = "adjudicate"
	GateSectionExternal   = "external"
	GateSectionUnknown    = "unknown"
)

func GateSectionOf(e domain.GateListEntry) string {
	kind := domain.GateKindOf(e.Labels, e.Fields.Subject)
	if kind == domain.GateKindHuman && len(UnknownWaitLabels(e.Labels)) > 0 {
		return GateSectionUnknown
	}
	if len(GateProbeLabels(e.Labels, kind)) > 0 {
		return GateSectionExternal
	}
	switch kind {
	case domain.GateKindHuman:
		if len(e.PrereqBlockers) > 0 {
			return GateSectionPrereq
		}
		return GateSectionHuman
	case domain.GateKindAdjudicate:
		return GateSectionAdjudicate
	case domain.GateKindExternal:
		return GateSectionExternal
	default:
		return GateSectionUnknown
	}
}

func GateProbeLabels(labels []string, kind string) []string {
	var probes []string
	for _, l := range labels {
		if !IsProbe(l) {
			continue
		}
		if (kind == domain.GateKindAdjudicate || kind == domain.GateKindHuman) && WaitsOnPerson(l) {
			continue
		}
		if kind == domain.GateKindHuman && strings.HasPrefix(l, File.Prefix()) {
			continue
		}
		probes = append(probes, l)
	}
	return probes
}

func UnknownWaitLabels(labels []string) []string {
	var unknown []string
	for _, l := range labels {
		if IsUnknown(l) {
			unknown = append(unknown, l)
		}
	}
	return unknown
}
