package memory

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ozon/internal/domain"
)

func TestRefreshCleanupIsBounded(t *testing.T) {
	t.Parallel()
	db := NewDatabase()
	repository := NewRefreshRepository(db)
	now := time.Now()
	for i := range 100 {
		hash := sha256.Sum256([]byte(fmt.Sprint(i)))
		db.refreshTokens[hash] = domain.RefreshToken{Hash: hash, ExpiresAt: now.Add(-time.Second)}
	}
	repository.cleanupExpired(now)
	require.Len(t, db.refreshTokens, 36)
	repository.cleanupExpired(now)
	require.Empty(t, db.refreshTokens)
	hash := sha256.Sum256([]byte("live"))
	db.refreshTokens[hash] = domain.RefreshToken{Hash: hash, ExpiresAt: now.Add(time.Hour)}
	repository.cleanupExpired(now)
	require.Len(t, db.refreshTokens, 1)
}
