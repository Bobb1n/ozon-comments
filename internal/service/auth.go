package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"ozon/internal/domain"
)

type UserRepository interface {
	FindUserByLogin(context.Context, string) (domain.User, error)
	CreateUser(context.Context, domain.User) error
}

type AuthToken struct {
	Token            string
	ExpiresAt        time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
}

type AuthOptions struct {
	Secret            string
	Issuer            string
	AccessTTL         time.Duration
	RefreshTTL        time.Duration
	PasswordCost      int
	MinPasswordLength int
}

type Identity struct {
	UserID    string
	ExpiresAt time.Time
}

type AuthService struct {
	users             UserRepository
	sessions          RefreshRepository
	refreshTTL        time.Duration
	passwordCost      int
	minPasswordLength int
	ttl               time.Duration
	secret            []byte
	issuer            string
	parser            *jwt.Parser
	dummyHash         []byte
}

func NewAuthService(users UserRepository, sessions RefreshRepository, options AuthOptions) (*AuthService, error) {
	if len(options.Secret) < 32 || strings.TrimSpace(options.Issuer) == "" || options.AccessTTL < time.Second || options.RefreshTTL <= options.AccessTTL {
		return nil, fmt.Errorf("JWT requires a secret of at least 32 bytes, an issuer and a TTL of at least one second")
	}
	if options.PasswordCost < bcrypt.MinCost || options.PasswordCost > 14 || options.MinPasswordLength < 8 || options.MinPasswordLength > 72 {
		return nil, fmt.Errorf("invalid password settings")
	}
	dummyHash, err := bcrypt.GenerateFromPassword([]byte(rand.Text()), options.PasswordCost)
	if err != nil {
		return nil, fmt.Errorf("initialize password verifier: %w", err)
	}
	return &AuthService{
		users: users, sessions: sessions, secret: []byte(options.Secret), issuer: options.Issuer, ttl: options.AccessTTL, dummyHash: dummyHash,
		refreshTTL: options.RefreshTTL, passwordCost: options.PasswordCost, minPasswordLength: options.MinPasswordLength,
		parser: jwt.NewParser(jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired(), jwt.WithIssuer(options.Issuer), jwt.WithIssuedAt()),
	}, nil
}

func (s *AuthService) Register(ctx context.Context, login, password string) (domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	login = strings.TrimSpace(login)
	if login == "" || len(login) > 128 || !utf8.ValidString(login) || !utf8.ValidString(password) || utf8.RuneCountInString(password) < s.minPasswordLength || len(password) > 72 {
		return domain.User{}, domain.ErrInvalidInput
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.passwordCost)
	if err != nil {
		return domain.User{}, fmt.Errorf("hash password: %w", err)
	}
	user := domain.User{ID: uuid.NewString(), Login: login, PasswordHash: string(hash)}
	if err := s.users.CreateUser(ctx, user); err != nil {
		return domain.User{}, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

func (s *AuthService) Login(ctx context.Context, login, password string) (AuthToken, error) {
	if err := ctx.Err(); err != nil {
		return AuthToken{}, err
	}
	if login == "" || len(login) > 128 || len(password) == 0 || len(password) > 72 {
		return AuthToken{}, domain.ErrUnauthenticated
	}
	user, err := s.users.FindUserByLogin(ctx, strings.TrimSpace(login))
	if errors.Is(err, domain.ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
		return AuthToken{}, domain.ErrUnauthenticated
	}
	if err != nil {
		return AuthToken{}, fmt.Errorf("find login user: %w", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return AuthToken{}, domain.ErrUnauthenticated
	}
	if err := ctx.Err(); err != nil {
		return AuthToken{}, err
	}
	return s.startSession(ctx, user.ID)
}

func (s *AuthService) Authenticate(ctx context.Context, token string) (Identity, error) {
	if err := ctx.Err(); err != nil {
		return Identity{}, err
	}
	var claims jwt.RegisteredClaims
	parsed, err := s.parser.ParseWithClaims(token, &claims, func(*jwt.Token) (any, error) {
		return s.secret, nil
	})
	if err != nil || !parsed.Valid || strings.TrimSpace(claims.Subject) == "" {
		return Identity{}, domain.ErrUnauthenticated
	}
	return Identity{UserID: claims.Subject, ExpiresAt: claims.ExpiresAt.Time}, nil
}

func (s *AuthService) issueToken(userID string) (AuthToken, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject: userID, Issuer: s.issuer, ID: uuid.NewString(),
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(s.ttl)),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return AuthToken{}, fmt.Errorf("sign access token: %w", err)
	}
	return AuthToken{Token: token, ExpiresAt: claims.ExpiresAt.Time}, nil
}
