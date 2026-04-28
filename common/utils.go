/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2023, daeuniverse Organization <team@v2raya.org>
 */

package common

import (
	"crypto/sha256"
	"encoding/hex"

	daeCommon "github.com/daeuniverse/dae/common"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func PasswordToHash32(entropy []byte, password string) string {
	h := sha256.New()
	h.Write([]byte("1b74413f-f3b8-409f-ak47-e8c062e3472a"))
	h.Write(entropy)
	h.Write([]byte(password))
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func GetIfAddrs() (globalIfAddrs []string, err error) {
	linkList, err := netlink.LinkList()
	if err != nil {
		return nil, err
	}
	for _, family := range []int{unix.AF_INET, unix.AF_INET6} {
		for _, link := range linkList {
			if link.Attrs().Flags&unix.RTF_UP != unix.RTF_UP {
				// Interface is down.
				continue
			}
			addrs, err := netlink.AddrList(link, family)
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				if addr.IP == nil ||
					addr.IP.IsUnspecified() ||
					addr.IP.IsInterfaceLocalMulticast() ||
					addr.IP.IsMulticast() ||
					addr.IP.IsLinkLocalMulticast() {
					continue
				}
				globalIfAddrs = append(globalIfAddrs, addr.IP.String())
			}
		}
	}
	return daeCommon.Deduplicate(globalIfAddrs), nil
}
