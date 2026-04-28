/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package orchestrator

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/engine"
	"github.com/daeuniverse/dae/common/subscription"
	"github.com/sirupsen/logrus"
)

func FetchSubscriptionLinks(subscriptionLink string) ([]string, error) {
	timeout := 10 * time.Second

	links, err := fetchSubscriptionLinksWithTransport(subscriptionLink, http.DefaultTransport, timeout/2)
	if err != nil {
		links, routeErr := fetchSubscriptionLinksWithTransport(subscriptionLink, engine.Default().HTTPTransport(), timeout/2)
		if routeErr != nil {
			if engine.Default().IsControlPlaneNotInit(routeErr) {
				return nil, err
			}
			return nil, fmt.Errorf("%v (direct); %w (route)", err, routeErr)
		}
		return links, nil
	}
	return links, nil
}

func fetchSubscriptionLinksWithTransport(subscriptionLink string, transport http.RoundTripper, timeout time.Duration) ([]string, error) {
	client := http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
	req, err := http.NewRequest(http.MethodGet, subscriptionLink, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", fmt.Sprintf("%v/%v (like v2rayA/1.0 WebRequestHelper) (like v2rayN/1.0 WebRequestHelper)", db.AppName, db.AppVersion))

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch link: %v", resp.Status)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	noLogger := logrus.New()
	noLogger.SetOutput(io.Discard)
	links, err := subscription.ResolveSubscriptionAsSIP008(noLogger, payload)
	if err != nil {
		links = subscription.ResolveSubscriptionAsBase64(noLogger, payload)
	}
	if len(links) == 0 {
		return nil, fmt.Errorf("fetched but no any node was found")
	}
	return links, nil
}
