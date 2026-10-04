package graphql

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ozon/internal/domain"
)

func TestCursorRoundTripAndScope(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	parent := "parent-1"
	scope := commentScope("post-1", &parent)
	encoded := encodeCursor(now, "comment-1", scope)
	page, err := decodePage(20, &encoded, scope, 100)
	require.NoError(t, err)
	require.Equal(t, domain.PageRequest{Limit: 20, After: &domain.PageCursor{CreatedAt: now, ID: "comment-1"}}, page)

	_, err = decodePage(20, &encoded, commentScope("post-1", nil), 100)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
	_, err = decodePage(20, &encoded, commentScope("post-2", &parent), 100)
	require.ErrorIs(t, err, domain.ErrInvalidInput)
}

func TestMalformedCursorAndPageSize(t *testing.T) {
	for _, invalid := range []string{"not-a-cursor", "", "e30"} {
		_, err := decodePage(20, &invalid, "posts", 100)
		require.ErrorIs(t, err, domain.ErrInvalidInput)
	}
	for _, size := range []int{-1, 0, 101} {
		_, err := decodePage(size, nil, "posts", 100)
		require.ErrorIs(t, err, domain.ErrInvalidInput)
	}
}

func TestCursorRejectsDifferentListAndIncompletePosition(t *testing.T) {
	t.Parallel()
	for _, encoded := range []string{
		base64.RawURLEncoding.EncodeToString([]byte("not json")),
		encodeCursor(time.Now(), "c", "other scope"),
		encodeCursor(time.Time{}, "c", "posts"),
		encodeCursor(time.Now(), "", "posts"),
	} {
		_, err := decodePage(10, &encoded, "posts", 100)
		require.ErrorIs(t, err, domain.ErrInvalidInput)
	}
}

func TestCursorRejectsUnsupportedDate(t *testing.T) {
	t.Parallel()
	require.Panics(t, func() { encodeCursor(time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), "c", "posts") })
}
