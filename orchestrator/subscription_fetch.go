/*
 * SPDX-License-Identifier: AGPL-3.0-only
 * Copyright (c) 2026, daeuniverse Organization <team@v2raya.org>
 */

package orchestrator

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/daeuniverse/dae-wing/db"
	"github.com/daeuniverse/dae-wing/engine"
	"github.com/daeuniverse/dae/common/subscription"
	"github.com/sirupsen/logrus"
)

const subscriptionFetchTimeout = 10 * time.Second

func FetchSubscriptionLinks(ctx context.Context, subscriptionLink string) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, subscriptionFetchTimeout)
	defer cancel()

	links, err := fetchSubscriptionLinksWithTransport(ctx, subscriptionLink, engine.Default().HTTPTransport())
	if err == nil {
		return links, nil
	}
	if !engine.Default().IsControlPlaneNotInit(err) {
		return nil, fmt.Errorf("failed to fetch subscription through route: %w", err)
	}
	links, directErr := fetchSubscriptionLinksWithTransport(ctx, subscriptionLink, http.DefaultTransport)
	if directErr != nil {
		return nil, fmt.Errorf("%v (route unavailable); %w (direct)", err, directErr)
	}
	return links, nil
}

func fetchSubscriptionLinksWithTransport(ctx context.Context, subscriptionLink string, transport http.RoundTripper) ([]string, error) {
	client := http.Client{
		Transport: transport,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, subscriptionLink, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", fmt.Sprintf("%v/%v (like v2rayA/1.0 WebRequestHelper) (like v2rayN/1.0 WebRequestHelper)", db.AppName, db.AppVersion))

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch link: %v", resp.Status)
	}

	payload, err := subscription.ReadAllLimited(resp.Body, subscription.MaxSubscriptionBytes)
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
