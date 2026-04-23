/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package node

import (
	"strings"

	httpdialer "github.com/daeuniverse/outbound/dialer/http"
	hysteria2dialer "github.com/daeuniverse/outbound/dialer/hysteria2"
	juicitydialer "github.com/daeuniverse/outbound/dialer/juicity"
	shadowsocksdialer "github.com/daeuniverse/outbound/dialer/shadowsocks"
	socksdialer "github.com/daeuniverse/outbound/dialer/socks"
	trojandialer "github.com/daeuniverse/outbound/dialer/trojan"
	tuicdialer "github.com/daeuniverse/outbound/dialer/tuic"
	v2raydialer "github.com/daeuniverse/outbound/dialer/v2ray"
)

func nodeTransport(protocol string, link string) *string {
	switch strings.ToLower(protocol) {
	case "vless":
		if parsed, err := v2raydialer.ParseVlessURL(link); err == nil {
			return transportLabel(parsed.Net, "tcp")
		}
	case "vmess":
		if parsed, err := v2raydialer.ParseVmessURL(link); err == nil {
			return transportLabel(parsed.Net, "tcp")
		}
	case "trojan", "trojan-go":
		if parsed, err := trojandialer.ParseTrojanURL(link); err == nil {
			return transportLabel(parsed.Type, "tcp")
		}
	case "shadowsocks":
		if parsed, err := shadowsocksdialer.ParseSSURL(link); err == nil {
			if parsed.Plugin.Name == "v2ray-plugin" {
				return transportLabel("ws", "")
			}
			return transportLabel("tcp", "")
		}
	case "tuic":
		if _, err := tuicdialer.ParseTuicURL(link); err == nil {
			return transportLabel("quic", "")
		}
	case "juicity":
		if _, err := juicitydialer.ParseJuicityURL(link); err == nil {
			return transportLabel("quic", "")
		}
	case "hysteria2":
		if _, err := hysteria2dialer.ParseHysteria2URL(link); err == nil {
			return transportLabel("quic", "")
		}
	case "anytls":
		return transportLabel("tcp", "")
	case "http", "https":
		if _, err := httpdialer.ParseHTTPURL(link); err == nil {
			return transportLabel("tcp", "")
		}
	case "socks5":
		if _, err := socksdialer.ParseSocksURL(link); err == nil {
			return transportLabel("tcp", "")
		}
	}

	return nil
}

func transportLabel(raw string, fallback string) *string {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		value = fallback
	}
	switch value {
	case "", "none":
		return nil
	case "websocket":
		value = "ws"
	case "meek":
		value = "http"
	}
	label := value
	return &label
}
