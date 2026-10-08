// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

const FileName = "config.json"

type Config struct {
	RetentionDays int

	ListLimit int

	NamespaceDefault string

	BusyTimeoutMS int

	SLOWarnMinutes int

	SLOIncidentMinutes int

	SLOTargetPercent int

	CreateRequireMilestone bool

	IDShortLen int
}

func (c *Config) ShortLenOr(def int) int {
	if c == nil {
		return def
	}
	return max(cmp.Or(c.IDShortLen, def), 3)
}

func (c *Config) RequireMilestone() bool {
	return c != nil && c.CreateRequireMilestone
}

func (c *Config) ListLimitOr(def int) int {
	if c == nil {
		return def
	}
	return cmp.Or(c.ListLimit, def)
}

func (c *Config) NamespaceDefaultOr(def string) string {
	if c == nil {
		return def
	}
	return cmp.Or(c.NamespaceDefault, def)
}

func (c *Config) BusyTimeoutMSOr(def int) int {
	if c == nil {
		return def
	}
	return cmp.Or(c.BusyTimeoutMS, def)
}

func (c *Config) RetentionDaysOr(def int) int {
	if c == nil {
		return def
	}
	return cmp.Or(c.RetentionDays, def)
}

type fileConfig struct {
	Note       string `json:"note"`
	Compaction struct {
		Note          string `json:"note"`
		RetentionDays int    `json:"retention_days"`
	} `json:"compaction"`
	List struct {
		Note  string `json:"note"`
		Limit int    `json:"limit"`
	} `json:"list"`
	Namespace struct {
		Note    string `json:"note"`
		Default string `json:"default"`
	} `json:"namespace"`
	DB struct {
		Note          string `json:"note"`
		BusyTimeoutMS int    `json:"busy_timeout_ms"`
	} `json:"db"`
	SLO struct {
		Note            string `json:"note"`
		WarnMinutes     int    `json:"warn_minutes"`
		IncidentMinutes int    `json:"incident_minutes"`
		TargetPercent   int    `json:"target_percent"`
	} `json:"slo"`
	Create struct {
		Note             string `json:"note"`
		RequireMilestone bool   `json:"require_milestone"`
	} `json:"create"`
	ID struct {
		Note     string `json:"note"`
		ShortLen int    `json:"short_len"`
	} `json:"id"`
}

func Load(path string) (*Config, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, false, nil
		}
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}

	cfg, err := parse(data, path)
	if err != nil {
		return nil, false, err
	}
	return cfg, true, nil
}

func parse(data []byte, path string) (*Config, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return &Config{}, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var fc fileConfig
	if err := dec.Decode(&fc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: unexpected data after the top-level object", path)
	}
	return &Config{
		RetentionDays:      fc.Compaction.RetentionDays,
		ListLimit:          fc.List.Limit,
		NamespaceDefault:   fc.Namespace.Default,
		BusyTimeoutMS:      fc.DB.BusyTimeoutMS,
		SLOWarnMinutes:     fc.SLO.WarnMinutes,
		SLOIncidentMinutes: fc.SLO.IncidentMinutes,
		SLOTargetPercent:   fc.SLO.TargetPercent,

		CreateRequireMilestone: fc.Create.RequireMilestone,
		IDShortLen:             fc.ID.ShortLen,
	}, nil
}
