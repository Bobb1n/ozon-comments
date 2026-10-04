package service

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"ozon/pkg/metrics"

	"github.com/stretchr/testify/require"

	"ozon/internal/domain"
)

func TestSubscriptionMetrics(t *testing.T) {
	t.Parallel()
	observer := metrics.New(true, "test")
	hub := NewCommentHub(1, slog.Default(), observer)
	ctx, cancel := context.WithCancel(t.Context())
	events := hub.Subscribe(ctx, "post")
	readMetrics := func() string {
		recorder := httptest.NewRecorder()
		observer.Handler().ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
		return recorder.Body.String()
	}
	require.Contains(t, readMetrics(), `subscriptions_active{service="test"} 1`)
	hub.Publish(domain.Comment{PostID: "post"})
	hub.Publish(domain.Comment{PostID: "post"})
	for range events {
	}
	cancel()
	require.Contains(t, readMetrics(), `subscriptions_active{service="test"} 0`)
	require.Contains(t, readMetrics(), `subscriptions_dropped_total{service="test"} 1`)
	ctx, stop := context.WithCancel(t.Context())
	events = hub.Subscribe(ctx, "post")
	stop()
	for range events {
	}
	require.Contains(t, readMetrics(), `subscriptions_active{service="test"} 0`)
	require.Contains(t, readMetrics(), `subscriptions_dropped_total{service="test"} 1`)
}

func TestIsolationAndCancellation(t *testing.T) {
	b := NewCommentHub(64, slog.Default(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := b.Subscribe(ctx, "post-1")
	second := b.Subscribe(ctx, "post-2")
	b.Publish(domain.Comment{ID: "c", PostID: "post-1"})
	select {
	case got := <-first:
		if got.ID != "c" {
			t.Fatal(got)
		}
	case <-time.After(time.Second):
		t.Fatal("missing event")
	}
	select {
	case <-second:
		t.Fatal("event leaked to another post")
	default:
	}
	cancel()
	select {
	case _, ok := <-first:
		if ok {
			t.Fatal("channel is open")
		}
	case <-time.After(time.Second):
		t.Fatal("channel was not closed")
	}
	b.Publish(domain.Comment{PostID: "post-1"})
}

func TestSlowSubscriberDoesNotBlockHealthySubscriber(t *testing.T) {
	t.Parallel()
	hub := NewCommentHub(1, slog.Default(), nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	slow, healthy := hub.Subscribe(ctx, "p"), hub.Subscribe(ctx, "p")
	hub.Publish(domain.Comment{ID: "first", PostID: "p"})
	require.Equal(t, "first", (<-healthy).ID)
	hub.Publish(domain.Comment{ID: "second", PostID: "p"})
	require.Equal(t, "second", (<-healthy).ID)
	require.Equal(t, "first", (<-slow).ID)
	_, open := <-slow
	require.False(t, open)
}

func TestConcurrentPublishAndCancellation(t *testing.T) {
	t.Parallel()
	hub := NewCommentHub(256, slog.Default(), nil)
	var workers sync.WaitGroup
	for range 50 {
		ctx, cancel := context.WithCancel(t.Context())
		events := hub.Subscribe(ctx, "p")
		workers.Go(func() {
			cancel()
			for range events {
			}
		})
	}
	for range 100 {
		workers.Go(func() { hub.Publish(domain.Comment{PostID: "p"}) })
	}
	workers.Wait()
	require.Eventually(t, func() bool {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		return len(hub.subscribers) == 0
	}, time.Second, time.Millisecond)
}

func TestSlowSubscriberIsDisconnected(t *testing.T) {
	b := NewCommentHub(64, slog.Default(), nil)
	ch := b.Subscribe(context.Background(), "p")
	for i := 0; i < 65; i++ {
		b.Publish(domain.Comment{PostID: "p"})
	}
	count := 0
	for range ch {
		count++
	}
	if count != 64 {
		t.Fatalf("received %d buffered events", count)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.subscribers) != 0 {
		t.Fatal("subscriber leaked")
	}
}
