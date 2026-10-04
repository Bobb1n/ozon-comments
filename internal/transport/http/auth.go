package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/labstack/echo/v5"
	echomiddleware "github.com/labstack/echo/v5/middleware"

	"ozon/internal/domain"
	"ozon/internal/service"
	"ozon/internal/transport/dto"
)

type LoginService interface {
	Login(context.Context, string, string) (service.AuthToken, error)
}
type RegistrationService interface {
	Register(context.Context, string, string) (domain.User, error)
}
type SessionService interface {
	Refresh(context.Context, string) (service.AuthToken, error)
	Logout(context.Context, string) error
}

type AuthOptions struct {
	Login        LoginService
	Registration RegistrationService
	Sessions     SessionService
	BodyLimit    int64
	RequestRate  float64
	RequestBurst int
}

func registerAuth(router *echo.Echo, options AuthOptions) {
	limiter := echomiddleware.RateLimiter(echomiddleware.NewRateLimiterMemoryStoreWithConfig(echomiddleware.RateLimiterMemoryStoreConfig{Rate: options.RequestRate, Burst: options.RequestBurst}))
	if options.Login != nil {
		router.POST("/auth/login", loginHandler(options), limiter)
	}
	if options.Registration != nil {
		router.POST("/auth/register", registrationHandler(options), limiter)
	}
	if options.Sessions != nil {
		router.POST("/auth/refresh", refreshHandler(options), limiter)
		router.POST("/auth/logout", logoutHandler(options), limiter)
	}
}

func registrationHandler(options AuthOptions) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input dto.Credentials
		if err := decodeAuthInput(c, options.BodyLimit, &input); err != nil {
			return err
		}
		user, err := options.Registration.Register(c.Request().Context(), input.Login, input.Password)
		if err != nil {
			return authHTTPError(c, err)
		}
		return c.JSON(http.StatusCreated, dto.UserResponse{ID: user.ID, Login: user.Login})
	}
}

func loginHandler(options AuthOptions) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input dto.Credentials
		if err := decodeAuthInput(c, options.BodyLimit, &input); err != nil {
			return err
		}
		token, err := options.Login.Login(c.Request().Context(), input.Login, input.Password)
		if err != nil {
			return authHTTPError(c, err)
		}
		c.Response().Header().Set("Cache-Control", "no-store")
		return c.JSON(http.StatusOK, newTokenResponse(token))
	}
}

func refreshHandler(options AuthOptions) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input dto.RefreshRequest
		if err := decodeAuthInput(c, options.BodyLimit, &input); err != nil {
			return err
		}
		token, err := options.Sessions.Refresh(c.Request().Context(), input.RefreshToken)
		if err != nil {
			return authHTTPError(c, err)
		}
		c.Response().Header().Set("Cache-Control", "no-store")
		return c.JSON(http.StatusOK, newTokenResponse(token))
	}
}

func logoutHandler(options AuthOptions) echo.HandlerFunc {
	return func(c *echo.Context) error {
		var input dto.RefreshRequest
		if err := decodeAuthInput(c, options.BodyLimit, &input); err != nil {
			return err
		}
		if err := options.Sessions.Logout(c.Request().Context(), input.RefreshToken); err != nil {
			return authHTTPError(c, err)
		}
		return c.NoContent(http.StatusNoContent)
	}
}

func decodeAuthInput(c *echo.Context, limit int64, target any) error {
	body := http.MaxBytesReader(c.Response(), c.Request().Body, limit)
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid authentication request")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid authentication request")
	}
	return nil
}

func authHTTPError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrUnauthenticated):
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
	case errors.Is(err, domain.ErrInvalidInput):
		return echo.NewHTTPError(http.StatusBadRequest, "invalid login or password")
	case errors.Is(err, domain.ErrConflict):
		return echo.NewHTTPError(http.StatusConflict, "login already exists")
	default:
		c.Logger().ErrorContext(c.Request().Context(), "authentication operation failed", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "internal server error")
	}
}

func newTokenResponse(token service.AuthToken) dto.TokenResponse {
	return dto.TokenResponse{
		Token: token.Token, ExpiresAt: token.ExpiresAt,
		RefreshToken: token.RefreshToken, RefreshExpiresAt: token.RefreshExpiresAt,
	}
}
