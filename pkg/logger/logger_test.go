package logger

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJSONLoggerFiltersByLevel(t *testing.T) {
	read, write, err := os.Pipe()
	require.NoError(t, err)
	original := os.Stdout
	os.Stdout = write
	t.Cleanup(func() { os.Stdout = original; _ = read.Close(); _ = write.Close() })
	log := NewLogger(slog.LevelWarn, "ozon-comments")
	log.Info("hidden")
	log.Error("failed", "operation", "create_post")
	require.NoError(t, write.Close())
	output, err := io.ReadAll(read)
	require.NoError(t, err)
	var entry map[string]any
	require.NoError(t, json.Unmarshal(output, &entry))
	require.Equal(t, "ozon-comments", entry["service"])
	require.Equal(t, "ERROR", entry["level"])
	require.Equal(t, "create_post", entry["operation"])
}
