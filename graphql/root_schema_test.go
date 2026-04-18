/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2023, daeuniverse Organization <team@v2raya.org>
 */

package graphql

import "testing"

func TestSchemaStringAndSchemaAreCached(t *testing.T) {
	s1, err := SchemaString()
	if err != nil {
		t.Fatalf("SchemaString failed: %v", err)
	}
	s2, err := SchemaString()
	if err != nil {
		t.Fatalf("SchemaString second call failed: %v", err)
	}
	if s1 != s2 {
		t.Fatal("expected repeated SchemaString calls to return identical schema text")
	}

	p1, err := Schema()
	if err != nil {
		t.Fatalf("Schema failed: %v", err)
	}
	p2, err := Schema()
	if err != nil {
		t.Fatalf("Schema second call failed: %v", err)
	}
	if p1 != p2 {
		t.Fatal("expected parsed schema to be cached and reused")
	}
}
