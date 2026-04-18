/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2023, daeuniverse Organization <team@v2raya.org>
 */

package dae

import (
	"errors"
	"testing"

	"github.com/daeuniverse/dae/control"
)

func TestControlPlaneAccessorUsesAtomicStore(t *testing.T) {
	storeControlPlane(nil)
	t.Cleanup(func() {
		storeControlPlane(nil)
	})

	if _, err := ControlPlane(); !errors.Is(err, ErrControlPlaneNotInit) {
		t.Fatalf("expected ErrControlPlaneNotInit, got %v", err)
	}

	expected := &control.ControlPlane{}
	storeControlPlane(expected)

	got, err := ControlPlane()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != expected {
		t.Fatal("expected accessor to return stored control plane")
	}
}
