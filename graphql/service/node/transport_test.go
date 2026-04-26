/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package node

import (
	"testing"

	shadowsocksrdialer "github.com/daeuniverse/outbound/dialer/shadowsocksr"
)

func TestNodeTransport(t *testing.T) {
	ssrLink := (&shadowsocksrdialer.ShadowsocksR{
		Name:     "demo",
		Server:   "example.com",
		Port:     8388,
		Password: "password",
		Cipher:   "aes-128-cfb",
		Proto:    "auth_chain_a",
		Obfs:     "tls1.2_ticket_auth",
		Protocol: "shadowsocksr",
	}).ExportToURL()

	tests := []struct {
		name     string
		protocol string
		link     string
		want     *string
	}{
		{
			name:     "vless ws",
			protocol: "vless",
			link:     "vless://uuid@example.com:443?type=ws&security=tls#demo",
			want:     strPtr("ws"),
		},
		{
			name:     "vmess grpc",
			protocol: "vmess",
			link:     "vmess://eyJhZGQiOiJleGFtcGxlLmNvbSIsInBvcnQiOiI0NDMiLCJpZCI6InV1aWQiLCJuZXQiOiJncnBjIiwicHMiOiJkZW1vIiwidGxzIjoidGxzIiwidiI6IjIiLCJwcm90byI6InZtZXNzIn0=",
			want:     strPtr("grpc"),
		},
		{
			name:     "trojan default tcp",
			protocol: "trojan",
			link:     "trojan://password@example.com:443#demo",
			want:     strPtr("tcp"),
		},
		{
			name:     "tuic quic",
			protocol: "tuic",
			link:     "tuic://uuid:password@example.com:443?congestion_control=bbr#demo",
			want:     strPtr("quic"),
		},
		{
			name:     "ss2022 label",
			protocol: "shadowsocks",
			link:     "ss://2022-blake3-aes-128-gcm:%2BmPWWGB%2F%2FPLkjgiXVeWszg%3D%3D@example.com:8388#demo",
			want:     strPtr("ss2022"),
		},
		{
			name:     "ssr proto and obfs",
			protocol: "shadowsocksr",
			link:     ssrLink,
			want:     strPtr("auth_chain_a · tls1.2_ticket_auth"),
		},
		{
			name:     "invalid link",
			protocol: "vless",
			link:     "not-a-link",
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := nodeTransport(tt.protocol, tt.link)
			if tt.want == nil {
				if got != nil {
					t.Fatalf("expected nil transport, got %q", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected transport %q, got nil", *tt.want)
			}
			if *got != *tt.want {
				t.Fatalf("expected transport %q, got %q", *tt.want, *got)
			}
		})
	}
}

func strPtr(value string) *string {
	return &value
}
