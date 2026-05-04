package orchestrator

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/daeuniverse/dae/common/subscription"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type closeTrackingBody struct {
	*strings.Reader
	closed bool
}

func (b *closeTrackingBody) Close() error {
	b.closed = true
	return nil
}

func TestFetchSubscriptionLinksWithTransportUsesRequestContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if _, ok := req.Context().Deadline(); !ok {
			t.Fatal("request context has no deadline")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader("c3M6Ly9leGFtcGxlCg==")),
		}, nil
	})

	links, err := fetchSubscriptionLinksWithTransport(ctx, "https://example.test/sub", transport)
	if err != nil {
		t.Fatalf("fetchSubscriptionLinksWithTransport() error = %v", err)
	}
	if len(links) != 1 || links[0] != "ss://example" {
		t.Fatalf("links = %#v, want ss://example", links)
	}
}

func TestFetchSubscriptionLinksWithTransportClosesNonOKBody(t *testing.T) {
	body := &closeTrackingBody{Reader: strings.NewReader("error")}
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Status:     "500 Internal Server Error",
			Body:       body,
		}, nil
	})

	_, err := fetchSubscriptionLinksWithTransport(context.Background(), "https://example.test/sub", transport)
	if err == nil {
		t.Fatal("fetchSubscriptionLinksWithTransport() succeeded on non-OK response")
	}
	if !body.closed {
		t.Fatal("non-OK response body was not closed")
	}
}

func TestFetchSubscriptionLinksWithTransportRejectsOversizeBody(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := strings.NewReader(strings.Repeat("a", int(subscription.MaxSubscriptionBytes)+1))
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(body),
		}, nil
	})

	_, err := fetchSubscriptionLinksWithTransport(context.Background(), "https://example.test/sub", transport)
	if err == nil {
		t.Fatal("fetchSubscriptionLinksWithTransport() accepted an oversized subscription")
	}
}
