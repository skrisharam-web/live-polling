// Package services holds the business rules. Services are the only layer that
// decides what is allowed; handlers do HTTP, repositories do persistence.
//
// Nothing here imports Gin, which is deliberate: it keeps the rules testable
// without an HTTP server, and it stops request-shaped concerns leaking into the
// domain.
package services

import (
	"context"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
	"github.com/skrisharam-web/live-polling/backend/internal/models"
	"github.com/skrisharam-web/live-polling/backend/internal/validation"
)

// UserStore is the slice of persistence the auth service needs. Depending on an
// interface rather than the concrete repository is what lets the unit tests below
// run without a database.
type UserStore interface {
	Create(ctx context.Context, user *models.User) error
	FindByEmail(ctx context.Context, email string) (*models.User, error)
	FindByID(ctx context.Context, id bson.ObjectID) (*models.User, error)
}

const (
	// defaultBcryptCost 12 is roughly 250ms per hash on current hardware: slow
	// enough to make offline cracking expensive, fast enough that a login does not
	// feel slow.
	defaultBcryptCost = 12

	tokenIssuer = "live-polling"
)

// AuthService registers accounts, authenticates them and issues session tokens.
type AuthService struct {
	users     UserStore
	jwtSecret []byte
	jwtExpiry time.Duration

	// now is injectable so token expiry can be tested without sleeping.
	now func() time.Time
	// bcryptCost is a field rather than a constant so tests can drop it to the
	// minimum. Hashing at cost 12 is the right choice in production and the wrong
	// one in a test suite that hashes hundreds of times, especially under -race.
	bcryptCost int
	// dummyHash is compared against when no account matches, so that a request for
	// an unknown address costs the same time as one for a known address. Without
	// it, response timing alone would reveal which addresses are registered.
	dummyHash []byte
}

// NewAuthService builds the service. The secret comes from configuration, which
// has already refused to start the process with a weak one in production.
func NewAuthService(users UserStore, jwtSecret string, jwtExpiry time.Duration) *AuthService {
	return newAuthService(users, jwtSecret, jwtExpiry, defaultBcryptCost)
}

func newAuthService(users UserStore, jwtSecret string, jwtExpiry time.Duration, cost int) *AuthService {
	dummy, err := bcrypt.GenerateFromPassword([]byte("dummy-password-for-constant-time-login"), cost)
	if err != nil {
		// GenerateFromPassword only fails on an invalid cost, which is a constant here.
		panic("bcrypt is unusable: " + err.Error())
	}

	return &AuthService{
		users:      users,
		jwtSecret:  []byte(jwtSecret),
		jwtExpiry:  jwtExpiry,
		now:        func() time.Time { return time.Now().UTC() },
		bcryptCost: cost,
		dummyHash:  dummy,
	}
}

// RegisterInput is the raw, untrusted registration payload.
type RegisterInput struct {
	Name     string
	Email    string
	Password string
}

// LoginInput is the raw, untrusted login payload.
type LoginInput struct {
	Email    string
	Password string
}

// Session is what a successful register or login produces.
type Session struct {
	User      *models.User
	Token     string
	ExpiresAt time.Time
}

// Register creates an account and immediately signs the user in.
func (s *AuthService) Register(ctx context.Context, input RegisterInput) (*Session, error) {
	v := validation.New()
	name := v.Name("name", input.Name)
	email := v.Email("email", input.Email)
	v.Password("password", input.Password)
	if !v.Valid() {
		return nil, apperr.Validation("Please correct the highlighted fields.", v.Fields())
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), s.bcryptCost)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "Could not create the account.", err)
	}

	now := s.now()
	user := &models.User{
		Name:         name,
		Email:        email,
		PasswordHash: string(hash),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	// Uniqueness is enforced by the index inside Create, so there is no
	// check-then-insert race here.
	if err := s.users.Create(ctx, user); err != nil {
		return nil, err
	}

	return s.newSession(user)
}

// Login verifies credentials and issues a session.
//
// Every failure path returns the same message and takes comparable time, so this
// endpoint cannot be used to enumerate which addresses have accounts.
func (s *AuthService) Login(ctx context.Context, input LoginInput) (*Session, error) {
	v := validation.New()
	email := v.Email("email", input.Email)
	v.Required("password", input.Password)
	if !v.Valid() {
		return nil, apperr.New(apperr.CodeUnauthorized, "Incorrect e-mail address or password.")
	}

	user, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		if apperr.Is(err, apperr.CodeNotFound) {
			// Spend the same work as a real comparison before failing.
			_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(input.Password))
			return nil, apperr.New(apperr.CodeUnauthorized, "Incorrect e-mail address or password.")
		}
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(input.Password)); err != nil {
		return nil, apperr.New(apperr.CodeUnauthorized, "Incorrect e-mail address or password.")
	}

	return s.newSession(user)
}

// UserFromToken resolves the account behind a session token. It is what the auth
// middleware calls on every protected request.
func (s *AuthService) UserFromToken(ctx context.Context, tokenString string) (*models.User, error) {
	unauthorised := apperr.New(apperr.CodeUnauthorized, "Please sign in to continue.")

	if tokenString == "" {
		return nil, unauthorised
	}

	claims := &jwt.RegisteredClaims{}
	// Pinning the accepted algorithm closes the "alg" confusion attack, where a
	// token is re-signed with "none" or with an asymmetric algorithm the verifier
	// would otherwise accept using the public key as an HMAC secret.
	token, err := jwt.ParseWithClaims(tokenString, claims, func(*jwt.Token) (any, error) {
		return s.jwtSecret, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !token.Valid {
		return nil, unauthorised
	}

	userID, err := bson.ObjectIDFromHex(claims.Subject)
	if err != nil {
		return nil, unauthorised
	}

	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		// A token for an account that no longer exists is not an internal error;
		// from the caller's point of view the session is simply invalid.
		if apperr.Is(err, apperr.CodeNotFound) {
			return nil, unauthorised
		}
		return nil, err
	}
	return user, nil
}

// newSession mints a signed token for a user.
func (s *AuthService) newSession(user *models.User) (*Session, error) {
	now := s.now()
	expiresAt := now.Add(s.jwtExpiry)

	claims := jwt.RegisteredClaims{
		Subject:   user.ID.Hex(),
		Issuer:    tokenIssuer,
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
	if err != nil {
		return nil, apperr.Wrap(apperr.CodeInternal, "Could not start your session.", err)
	}

	return &Session{User: user, Token: signed, ExpiresAt: expiresAt}, nil
}
