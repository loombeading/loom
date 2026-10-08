// Copyright 2026 Takanao Endo
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/loombeading/loom/internal/domain"
	"github.com/loombeading/loom/internal/output"
	"github.com/loombeading/loom/internal/storage"
)

func reportIfBusy(stderr io.Writer, busyTimeoutMS int, err error) bool {
	if !storage.IsBusyErr(err) {
		return false
	}
	fmt.Fprintf(stderr, "Busy: lock wait exceeded %dms\n", busyTimeoutMS)
	return true
}

func reportIfReadOnly(stderr io.Writer, id string, err error) bool {
	if !errors.Is(err, storage.ErrReadOnlyWrite) {
		return false
	}
	output.WriteRejected(stderr, id, err.Error())
	return true
}

func writeRejected(stdout, stderr io.Writer, id string, err error) {
	output.WriteRejected(stdout, id, err.Error())
	if _, ok := errors.AsType[*domain.TerminalInvariantError](err); ok {
		output.WriteError(stderr, errorWithHint(err))
	}
}

func errorWithHint(err error) string {
	if inv, ok := errors.AsType[*domain.TerminalInvariantError](err); ok {
		return inv.Reason + ": " + inv.Hint
	}
	return err.Error()
}

func reportIfGateTerminal(stderr io.Writer, id string, err error) bool {
	var noOwner *domain.WaitTargetNoOwnerError
	var noMaterial *domain.GateMaterialRequiredError
	if !errors.Is(err, domain.ErrGateTerminalOnlyResolve) && !errors.Is(err, domain.ErrGateNotAdjudicated) &&
		!errors.As(err, &noOwner) && !errors.As(err, &noMaterial) {
		return false
	}
	output.WriteRejected(stderr, id, err.Error())
	return true
}
