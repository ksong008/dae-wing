/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package httpapi

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

func encodeLegacyCursor(id uint) string {
	return base64.StdEncoding.WithPadding(base64.NoPadding).EncodeToString([]byte(fmt.Sprintf("cursor%d", id)))
}

func decodeLegacyCursor(cursor string) (uint, error) {
	raw, err := base64.StdEncoding.WithPadding(base64.NoPadding).DecodeString(cursor)
	if err != nil {
		return 0, fmt.Errorf("failed to parse cursor")
	}
	value, err := strconv.Atoi(strings.TrimPrefix(string(raw), "cursor"))
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("failed to parse cursor")
	}
	return uint(value), nil
}
