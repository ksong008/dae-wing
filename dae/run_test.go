/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2023, daeuniverse Organization <team@v2raya.org>
 */

package dae

import (
	"bytes"
	"errors"
	"testing"

	"github.com/daeuniverse/dae/control"
	"github.com/sirupsen/logrus"
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

func TestReconfigureLoggersPreservesOutput(t *testing.T) {
	var buf bytes.Buffer
	log := logrus.New()
	log.SetOutput(&buf)

	reconfigureLoggers(log, "debug", true)

	if log.Out != &buf {
		t.Fatal("expected logger output to be preserved")
	}
	if log.Level != logrus.DebugLevel {
		t.Fatalf("expected debug level, got %v", log.Level)
	}
}

func TestNotifyReloadCallbackHandlesNilAndBufferedChannels(t *testing.T) {
	notifyReloadCallback(nil, nil)

	ch := make(chan error, 1)
	expected := errors.New("reload failed")
	notifyReloadCallback(ch, expected)

	select {
	case got := <-ch:
		if !errors.Is(got, expected) {
			t.Fatalf("expected %v, got %v", expected, got)
		}
	default:
		t.Fatal("expected callback result to be delivered")
	}
}
