package service

import (
	"context"
	"log/slog"
	"sync"

	"ozon/internal/domain"
)

type subscriber struct {
	events chan domain.Comment
	cancel context.CancelFunc
}

type postSubscribers map[*subscriber]struct{}

type SubscriptionMetrics interface {
	SubscriptionOpened()
	SubscriptionClosed()
	SubscriptionDropped()
}

type CommentHub struct {
	mu          sync.Mutex
	bufferSize  int
	logger      *slog.Logger
	metrics     SubscriptionMetrics
	subscribers map[string]postSubscribers
}

func NewCommentHub(bufferSize int, logger *slog.Logger, metrics SubscriptionMetrics) *CommentHub {
	return &CommentHub{bufferSize: bufferSize, logger: logger, metrics: metrics, subscribers: make(map[string]postSubscribers)}
}

func (h *CommentHub) Subscribe(ctx context.Context, postID string) <-chan domain.Comment {
	ctx, cancel := context.WithCancel(ctx)
	subscriber := &subscriber{events: make(chan domain.Comment, h.bufferSize), cancel: cancel}
	h.mu.Lock()
	if h.subscribers[postID] == nil {
		h.subscribers[postID] = make(postSubscribers)
	}
	h.subscribers[postID][subscriber] = struct{}{}
	if h.metrics != nil {
		h.metrics.SubscriptionOpened()
	}
	h.mu.Unlock()
	h.logger.DebugContext(ctx, "comment subscription registered", "post_id", postID)
	go h.waitForCancellation(ctx, postID, subscriber)
	return subscriber.events
}

func (h *CommentHub) Publish(comment domain.Comment) {
	h.mu.Lock()
	disconnected := 0
	for subscriber := range h.subscribers[comment.PostID] {
		select {
		case subscriber.events <- comment:
		default:
			h.remove(comment.PostID, subscriber)
			if h.metrics != nil {
				h.metrics.SubscriptionDropped()
			}
			disconnected++
		}
	}
	h.mu.Unlock()
	if disconnected > 0 {
		h.logger.Warn("slow comment subscribers disconnected", "post_id", comment.PostID, "count", disconnected)
	}
}

func (h *CommentHub) waitForCancellation(ctx context.Context, postID string, subscriber *subscriber) {
	<-ctx.Done()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.remove(postID, subscriber)
}

func (h *CommentHub) remove(postID string, subscriber *subscriber) {
	if _, exists := h.subscribers[postID][subscriber]; !exists {
		return
	}
	delete(h.subscribers[postID], subscriber)
	if h.metrics != nil {
		h.metrics.SubscriptionClosed()
	}
	close(subscriber.events)
	subscriber.cancel()
	if len(h.subscribers[postID]) == 0 {
		delete(h.subscribers, postID)
	}
}
