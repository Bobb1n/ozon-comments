package httpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"ozon/internal/domain"
	"ozon/internal/service"
	"ozon/mocks"
)

func TestLoginEndpoint(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{
		{"success", nil, 200}, {"bad credentials", domain.ErrUnauthenticated, 401}, {"internal error", errors.New("database secret"), 500},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var output bytes.Buffer
			login := mocks.NewLoginService(t)
			token := service.AuthToken{Token: "private-token", ExpiresAt: time.Now().Add(time.Hour)}
			login.On("Login", mock.Anything, "author", "private-password").Return(token, test.err).Once()
			router := NewRouter(http.NotFoundHandler(), slog.New(slog.NewJSONHandler(&output, nil)), AuthOptions{Login: login, BodyLimit: 1024, RequestRate: 100, RequestBurst: 10}, MetricsOptions{})
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"login":"author","password":"private-password"}`)))
			require.Equal(t, test.status, recorder.Code)
			require.NotContains(t, output.String(), "private-token")
			require.NotContains(t, output.String(), "private-password")
			require.NotContains(t, recorder.Body.String(), "database secret")
			if test.err == nil {
				var got service.AuthToken
				require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &got))
				require.Equal(t, token.Token, got.Token)
				require.WithinDuration(t, token.ExpiresAt, got.ExpiresAt, 0)
				require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestLoginBadBodyAndRateLimit(t *testing.T) {
	t.Parallel()
	login := mocks.NewLoginService(t)
	log := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
	router := NewRouter(http.NotFoundHandler(), log, AuthOptions{Login: login, BodyLimit: 64, RequestRate: 100, RequestBurst: 10}, MetricsOptions{})
	for _, body := range []string{"broken", strings.Repeat(" ", 100) + `{}`, `{} {}`, `{"unknown":true}`} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(body)))
		require.Equal(t, http.StatusBadRequest, recorder.Code)
	}
	login.On("Login", mock.Anything, "", "").Return(service.AuthToken{}, domain.ErrUnauthenticated).Once()
	router = NewRouter(http.NotFoundHandler(), log, AuthOptions{Login: login, BodyLimit: 64, RequestRate: 0.0001, RequestBurst: 1}, MetricsOptions{})
	for i := range 2 {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{}`))
		req.Header.Set("X-Forwarded-For", []string{"1.1.1.1", "2.2.2.2"}[i])
		router.ServeHTTP(recorder, req)
		if i == 0 {
			require.Equal(t, 401, recorder.Code)
		} else {
			require.Equal(t, 429, recorder.Code)
		}
	}
}

func TestRegistrationEndpoint(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{
		{"success", nil, 201}, {"invalid", domain.ErrInvalidInput, 400}, {"duplicate", domain.ErrConflict, 409}, {"internal", errors.New("database-secret"), 500},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			registration := mocks.NewRegistrationService(t)
			registration.On("Register", mock.Anything, "author", "test-password").Return(domain.User{ID: "id", Login: "author", PasswordHash: "hidden-hash"}, test.err).Once()
			var output bytes.Buffer
			router := NewRouter(http.NotFoundHandler(), slog.New(slog.NewJSONHandler(&output, nil)), AuthOptions{Registration: registration, BodyLimit: 1024, RequestRate: 100, RequestBurst: 10}, MetricsOptions{})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(`{"login":"author","password":"test-password"}`)))
			require.Equal(t, test.status, response.Code)
			if test.err == nil {
				require.JSONEq(t, `{"id":"id","login":"author"}`, response.Body.String())
			}
			require.NotContains(t, response.Body.String(), "hidden-hash")
			require.NotContains(t, response.Body.String(), "database-secret")
			require.NotContains(t, output.String(), "test-password")
		})
	}
}

func TestSessionEndpoints(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/auth/refresh", "/auth/logout"} {
		for _, test := range []struct {
			name string
			err  error
		}{
			{"success", nil}, {"invalid", domain.ErrUnauthenticated}, {"internal", errors.New("database-secret")},
		} {
			t.Run(path+test.name, func(t *testing.T) {
				t.Parallel()
				sessions := mocks.NewSessionService(t)
				status := http.StatusOK
				if path == "/auth/refresh" {
					sessions.On("Refresh", mock.Anything, "refresh-secret").Return(service.AuthToken{Token: "access-secret", RefreshToken: "next-secret"}, test.err).Once()
				} else {
					sessions.On("Logout", mock.Anything, "refresh-secret").Return(test.err).Once()
					status = http.StatusNoContent
				}
				if test.err == domain.ErrUnauthenticated {
					status = 401
				} else if test.err != nil {
					status = 500
				}
				var output bytes.Buffer
				router := NewRouter(http.NotFoundHandler(), slog.New(slog.NewJSONHandler(&output, nil)), AuthOptions{Sessions: sessions, BodyLimit: 1024, RequestRate: 100, RequestBurst: 10}, MetricsOptions{})
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"refreshToken":"refresh-secret"}`)))
				require.Equal(t, status, response.Code)
				require.NotContains(t, output.String(), "refresh-secret")
				require.NotContains(t, output.String(), "access-secret")
				require.NotContains(t, output.String(), "next-secret")
				require.NotContains(t, response.Body.String(), "database-secret")
			})
		}
	}
}

func TestAuthEndpointsRejectInvalidBodies(t *testing.T) {
	t.Parallel()
	registration, sessions := mocks.NewRegistrationService(t), mocks.NewSessionService(t)
	router := NewRouter(http.NotFoundHandler(), slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), AuthOptions{Registration: registration, Sessions: sessions, BodyLimit: 64, RequestRate: 100, RequestBurst: 10}, MetricsOptions{})
	for _, path := range []string{"/auth/register", "/auth/refresh", "/auth/logout"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, strings.NewReader("broken")))
		require.Equal(t, 400, response.Code)
	}
}
