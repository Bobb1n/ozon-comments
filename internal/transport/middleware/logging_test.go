package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func TestLoggingStatusAndSensitiveData(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		status       int
		level        string
		handlerError bool
	}{
		{200, "INFO", false}, {400, "WARN", false}, {500, "ERROR", false}, {503, "ERROR", true},
	} {
		t.Run(test.level+http.StatusText(test.status), func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			router := echo.New()
			router.Use(Logging(slog.New(slog.NewJSONHandler(&output, nil))))
			router.GET("/test", func(c *echo.Context) error {
				if test.handlerError {
					return echo.NewHTTPError(test.status, "unavailable")
				}
				return c.String(test.status, "response")
			})
			req := httptest.NewRequest(http.MethodGet, "/test?secret=hidden", nil)
			req.Header.Set("Authorization", "Bearer private-token")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			require.Equal(t, test.status, recorder.Code)
			var record map[string]any
			require.NoError(t, json.Unmarshal(output.Bytes(), &record))
			require.Equal(t, test.level, record["level"])
			require.Equal(t, float64(test.status), record["status"])
			require.Equal(t, "/test", record["path"])
			require.NotContains(t, output.String(), "hidden")
			require.NotContains(t, output.String(), "private-token")
		})
	}
}
