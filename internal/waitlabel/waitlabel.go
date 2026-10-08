// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package waitlabel

import (
	"maps"
	"slices"
	"strings"
)

const Prefix = "wait:"

type Kind string

const (
	Disk     Kind = "disk"
	Date     Kind = "date"
	File     Kind = "file"
	CIGreen  Kind = "ci-green"
	Runs     Kind = "runs"
	Proofs   Kind = "proofs"
	Log      Kind = "log"
	PRMerged Kind = "pr-merged"
	After    Kind = "after"
	Closes   Kind = "closes"
	Tier     Kind = "tier"

	RepoVisibility Kind = "repo-visibility"
	RepoAbsent     Kind = "repo-absent"
	GitHub         Kind = "github"

	Tag           Kind = "tag"
	SLI           Kind = "sli"
	PublicPath    Kind = "public-path"
	PublicReview  Kind = "public-review"
	PublicCheck   Kind = "public-check"
	SchemaVersion Kind = "schema-version"
	URL           Kind = "url"

	RunnersOnline Kind = "runners-online"
)

func (k Kind) Prefix() string { return Prefix + string(k) + ":" }

var probeKinds = map[Kind]bool{
	Disk: false, Date: false, File: false, CIGreen: false,
	Runs: false, Proofs: false, Log: false, After: false, Closes: false,
	Tier: false, PRMerged: true, RepoVisibility: true,
	RepoAbsent: true, GitHub: false,
	Tag: false, SLI: false, PublicPath: false, PublicReview: false, PublicCheck: false,
	SchemaVersion: false, URL: false,
}

var Kinds = slices.Sorted(maps.Keys(probeKinds))

func kindOf(label string) (Kind, bool) {
	rest, ok := strings.CutPrefix(label, Prefix)
	if !ok {
		return "", false
	}
	kind, _, _ := strings.Cut(rest, ":")
	return Kind(kind), true
}

func IsProbe(label string) bool {
	kind, ok := kindOf(label)
	if !ok {
		return false
	}
	_, ok = probeKinds[kind]
	return ok
}

func WaitsOnPerson(label string) bool {
	kind, ok := kindOf(label)
	return ok && probeKinds[kind]
}

const Any = Prefix + "any"

func IsUnknown(label string) bool {
	return label != Any && strings.HasPrefix(label, Prefix) && !IsProbe(label)
}
