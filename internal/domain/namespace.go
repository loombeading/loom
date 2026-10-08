// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"errors"
	"fmt"
)

const DefaultNamespace = "id"

func ValidateNamespace(s string) error {
	if s == "" {
		return errors.New("namespace must not be empty")
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			continue
		case i > 0 && r >= '0' && r <= '9':
			continue
		default:
			return fmt.Errorf("namespace %q: must start with a lowercase letter and contain only lowercase letters and digits", s)
		}
	}
	return nil
}

func validateOrErr(s string) (string, error) {
	if err := ValidateNamespace(s); err != nil {
		return "", err
	}
	return s, nil
}

func ResolveNamespaceForCreate(flag string, env func(string) string, defaultNamespace string) (string, error) {
	if flag != "" {
		return validateOrErr(flag)
	}
	if v := env("LM_NAMESPACE"); v != "" {
		return validateOrErr(v)
	}
	return validateOrErr(defaultNamespace)
}

func ResolveNamespaceForFilter(flag string, flagSet bool, env func(string) string, defaultNamespace string) (string, error) {
	if flagSet {
		if flag == "" {
			return "", nil
		}
		return validateOrErr(flag)
	}
	if v := env("LM_NAMESPACE"); v != "" {
		return validateOrErr(v)
	}
	if defaultNamespace != "" {
		return validateOrErr(defaultNamespace)
	}
	return "", nil
}
