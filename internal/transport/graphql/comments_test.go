package graphql

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ozon/internal/domain"
)

func TestForwardComments(t *testing.T) {
	t.Parallel()
	for _, test := range []string{"source closed", "context canceled", "blocked receiver"} {
		t.Run(test, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			source := make(chan domain.Comment)
			output := make(chan *domain.Comment)
			finished := make(chan struct{})
			go func() { forwardComments(ctx, cancel, source, output); close(finished) }()
			switch test {
			case "source closed":
				source <- domain.Comment{ID: "c"}
				select {
				case got := <-output:
					require.Equal(t, "c", got.ID)
				case <-time.After(time.Second):
					t.Fatal("missing comment")
				}
				close(source)
			case "context canceled":
				cancel()
			case "blocked receiver":
				source <- domain.Comment{ID: "c"}
				cancel()
			}
			select {
			case <-finished:
			case <-time.After(time.Second):
				t.Fatal("forwarder leaked")
			}
			_, open := <-output
			require.False(t, open)
			require.ErrorIs(t, ctx.Err(), context.Canceled)
		})
	}
}
