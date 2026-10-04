package graphql

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPageComplexity(t *testing.T) {
	t.Parallel()
	complexity := pageComplexity(100, 1000)
	for _, test := range []struct{ children, size, want int }{
		{2, 1, 3}, {2, 100, 201}, {0, 100, 101}, {1000, 100, 1001}, {2, 0, 1}, {2, -1, 1}, {2, 101, 1},
	} {
		require.Equal(t, test.want, complexity.Query.Posts(test.children, test.size, nil))
		require.Equal(t, test.want, complexity.Query.Comments(test.children, "p", nil, test.size, nil))
	}
}
