package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"

	"github.com/skrisharam-web/live-polling/backend/internal/apperr"
	"github.com/skrisharam-web/live-polling/backend/internal/models"
)

// fakeUserStore is an in-memory stand-in for the user repository. The repository
// itself is covered against real MongoDB in tests/integration; here the point is
// to exercise the rules, so a map keeps the tests fast and deterministic.
type fakeUserStore struct {
	byEmail  map[string]*models.User
	byID     map[bson.ObjectID]*models.User
	failNext error
}

func newFakeUserStore() *fakeUserStore {
	return &fakeUserStore{
		byEmail: make(map[string]*models.User),
		byID:    make(map[bson.ObjectID]*models.User),
	}
}

func (f *fakeUserStore) Create(_ context.Context, user *models.User) error {
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return err
	}
	if _, exists := f.byEmail[user.Email]; exists {
		return apperr.New(apperr.CodeConflict, "An account with that e-mail address already exists.")
	}
	user.ID = bson.NewObjectID()
	f.byEmail[user.Email] = user
	f.byID[user.ID] = user
	return nil
}

func (f *fakeUserStore) FindByEmail(_ context.Context, email string) (*models.User, error) {
	if user, ok := f.byEmail[email]; ok {
		return user, nil
	}
	return nil, apperr.New(apperr.CodeNotFound, "Account not found.")
}

func (f *fakeUserStore) FindByID(_ context.Context, id bson.ObjectID) (*models.User, error) {
	if user, ok := f.byID[id]; ok {
		return user, nil
	}
	return nil, apperr.New(apperr.CodeNotFound, "Account not found.")
}

func newTestAuthService(t *testing.T) (*AuthService, *fakeUserStore) {
	t.Helper()
	store := newFakeUserStore()
	// Cost 4 keeps the suite fast; the production cost is covered by
	// TestProductionUsesAStrongBcryptCost below.
	return newAuthService(store, "test-secret-that-is-long-enough-for-hs256", time.Hour, bcrypt.MinCost), store
}

func registerTestUser(t *testing.T, svc *AuthService) *Session {
	t.Helper()
	session, err := svc.Register(context.Background(), RegisterInput{
		Name:     "Ada Lovelace",
		Email:    "Ada@Example.com",
		Password: "correct-horse-battery",
	})
	if err != nil {
		t.Fatalf("Register() = %v", err)
	}
	return session
}

func TestRegister(t *testing.T) {
	svc, store := newTestAuthService(t)
	session := registerTestUser(t, svc)

	t.Run("the password is hashed, never stored as typed", func(t *testing.T) {
		stored := store.byEmail["ada@example.com"]
		if stored.PasswordHash == "correct-horse-battery" {
			t.Fatal("the plaintext password was stored")
		}
		if !strings.HasPrefix(stored.PasswordHash, "$2a$") && !strings.HasPrefix(stored.PasswordHash, "$2b$") {
			t.Errorf("stored hash %q does not look like bcrypt", stored.PasswordHash)
		}
		if err := bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte("correct-horse-battery")); err != nil {
			t.Errorf("the stored hash does not verify against the original password: %v", err)
		}
	})

	t.Run("the e-mail is normalised", func(t *testing.T) {
		if session.User.Email != "ada@example.com" {
			t.Errorf("email = %q, want the lower-cased form", session.User.Email)
		}
	})

	t.Run("a session token is issued", func(t *testing.T) {
		if session.Token == "" {
			t.Fatal("Register() returned no token")
		}
		if !session.ExpiresAt.After(time.Now()) {
			t.Error("the session expiry is not in the future")
		}
	})

	t.Run("a duplicate e-mail is refused", func(t *testing.T) {
		_, err := svc.Register(context.Background(), RegisterInput{
			Name:     "Someone Else",
			Email:    "ada@example.com",
			Password: "another-password",
		})
		if !apperr.Is(err, apperr.CodeConflict) {
			t.Fatalf("Register() with a duplicate e-mail = %v, want CONFLICT", err)
		}
	})
}

func TestRegisterValidation(t *testing.T) {
	svc, _ := newTestAuthService(t)

	cases := []struct {
		name      string
		input     RegisterInput
		wantField string
	}{
		{"empty name", RegisterInput{Name: "", Email: "a@example.com", Password: "password123"}, "name"},
		{"bad email", RegisterInput{Name: "Ada", Email: "not-an-email", Password: "password123"}, "email"},
		{"short password", RegisterInput{Name: "Ada", Email: "a@example.com", Password: "short"}, "password"},
		{"password beyond bcrypt's limit", RegisterInput{Name: "Ada", Email: "a@example.com", Password: strings.Repeat("a", 73)}, "password"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Register(context.Background(), tc.input)

			var domain *apperr.Error
			if !apperr.As(err, &domain) || domain.Code != apperr.CodeValidation {
				t.Fatalf("Register() = %v, want a VALIDATION_ERROR", err)
			}
			if _, ok := domain.Fields[tc.wantField]; !ok {
				t.Errorf("validation fields = %v, want an entry for %q", domain.Fields, tc.wantField)
			}
		})
	}
}

func TestLogin(t *testing.T) {
	svc, _ := newTestAuthService(t)
	registerTestUser(t, svc)

	t.Run("correct credentials", func(t *testing.T) {
		session, err := svc.Login(context.Background(), LoginInput{Email: "ada@example.com", Password: "correct-horse-battery"})
		if err != nil {
			t.Fatalf("Login() = %v", err)
		}
		if session.Token == "" {
			t.Error("Login() returned no token")
		}
	})

	t.Run("the e-mail is matched case-insensitively", func(t *testing.T) {
		if _, err := svc.Login(context.Background(), LoginInput{Email: "  ADA@EXAMPLE.COM ", Password: "correct-horse-battery"}); err != nil {
			t.Errorf("Login() with a differently-cased address = %v", err)
		}
	})

	// Every failure must be indistinguishable from the others: the message, the
	// code, and which account exists must not be inferable from the response.
	failures := []struct {
		name  string
		input LoginInput
	}{
		{"wrong password", LoginInput{Email: "ada@example.com", Password: "wrong-password"}},
		{"unknown account", LoginInput{Email: "nobody@example.com", Password: "correct-horse-battery"}},
		{"empty password", LoginInput{Email: "ada@example.com", Password: ""}},
		{"malformed email", LoginInput{Email: "not-an-email", Password: "correct-horse-battery"}},
	}

	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Login(context.Background(), tc.input)

			var domain *apperr.Error
			if !apperr.As(err, &domain) {
				t.Fatalf("Login() = %v, want a domain error", err)
			}
			if domain.Code != apperr.CodeUnauthorized {
				t.Errorf("code = %v, want UNAUTHORIZED", domain.Code)
			}
			if domain.Message != "Incorrect e-mail address or password." {
				t.Errorf("message = %q; every failure must return the same message so accounts cannot be enumerated", domain.Message)
			}
			if len(domain.Fields) != 0 {
				t.Errorf("fields = %v; a login failure must not say which field was wrong", domain.Fields)
			}
		})
	}
}

func TestUserFromToken(t *testing.T) {
	svc, store := newTestAuthService(t)
	session := registerTestUser(t, svc)

	t.Run("a valid token resolves to the user", func(t *testing.T) {
		user, err := svc.UserFromToken(context.Background(), session.Token)
		if err != nil {
			t.Fatalf("UserFromToken() = %v", err)
		}
		if user.ID != session.User.ID {
			t.Errorf("resolved user %v, want %v", user.ID, session.User.ID)
		}
	})

	rejected := []struct {
		name  string
		token func() string
	}{
		{"empty", func() string { return "" }},
		{"garbage", func() string { return "not.a.token" }},
		{"signed with a different secret", func() string {
			claims := jwt.RegisteredClaims{
				Subject:   session.User.ID.Hex(),
				Issuer:    tokenIssuer,
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			}
			signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("an-attackers-secret"))
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			return signed
		}},
		{"unsigned (alg none)", func() string {
			claims := jwt.RegisteredClaims{
				Subject:   session.User.ID.Hex(),
				Issuer:    tokenIssuer,
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			}
			signed, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			return signed
		}},
		{"wrong issuer", func() string {
			claims := jwt.RegisteredClaims{
				Subject:   session.User.ID.Hex(),
				Issuer:    "somebody-else",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			}
			signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(svc.jwtSecret)
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			return signed
		}},
		{"no expiry claim", func() string {
			claims := jwt.RegisteredClaims{Subject: session.User.ID.Hex(), Issuer: tokenIssuer}
			signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(svc.jwtSecret)
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			return signed
		}},
		{"subject is not an object id", func() string {
			claims := jwt.RegisteredClaims{
				Subject:   "not-an-object-id",
				Issuer:    tokenIssuer,
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			}
			signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(svc.jwtSecret)
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			return signed
		}},
	}

	for _, tc := range rejected {
		t.Run(tc.name+" is rejected", func(t *testing.T) {
			if _, err := svc.UserFromToken(context.Background(), tc.token()); !apperr.Is(err, apperr.CodeUnauthorized) {
				t.Fatalf("UserFromToken() = %v, want UNAUTHORIZED", err)
			}
		})
	}

	t.Run("an expired token is rejected", func(t *testing.T) {
		expiring := newAuthService(store, "test-secret-that-is-long-enough-for-hs256", time.Minute, bcrypt.MinCost)
		// Issue the token as though it were minted two hours ago.
		expiring.now = func() time.Time { return time.Now().UTC().Add(-2 * time.Hour) }

		expired, err := expiring.newSession(session.User)
		if err != nil {
			t.Fatalf("newSession() = %v", err)
		}
		if _, err := svc.UserFromToken(context.Background(), expired.Token); !apperr.Is(err, apperr.CodeUnauthorized) {
			t.Fatalf("UserFromToken() with an expired token = %v, want UNAUTHORIZED", err)
		}
	})

	t.Run("a token for a deleted account is rejected", func(t *testing.T) {
		delete(store.byID, session.User.ID)
		if _, err := svc.UserFromToken(context.Background(), session.Token); !apperr.Is(err, apperr.CodeUnauthorized) {
			t.Fatalf("UserFromToken() for a deleted account = %v, want UNAUTHORIZED", err)
		}
	})
}

func TestProductionUsesAStrongBcryptCost(t *testing.T) {
	// The cheap cost used elsewhere in this file must not be what ships. bcrypt's
	// cost is the only thing standing between a stolen database and the plaintext
	// passwords in it, so the default is asserted explicitly.
	svc := NewAuthService(newFakeUserStore(), "test-secret-that-is-long-enough-for-hs256", time.Hour)
	if svc.bcryptCost != defaultBcryptCost {
		t.Fatalf("default bcrypt cost = %d, want %d", svc.bcryptCost, defaultBcryptCost)
	}
	if defaultBcryptCost < 12 {
		t.Errorf("bcrypt cost %d is too low for production", defaultBcryptCost)
	}

	session, err := svc.Register(context.Background(), RegisterInput{
		Name: "Ada", Email: "ada@example.com", Password: "correct-horse-battery",
	})
	if err != nil {
		t.Fatalf("Register() = %v", err)
	}
	cost, err := bcrypt.Cost([]byte(session.User.PasswordHash))
	if err != nil {
		t.Fatalf("bcrypt.Cost() = %v", err)
	}
	if cost != defaultBcryptCost {
		t.Errorf("stored hash cost = %d, want %d", cost, defaultBcryptCost)
	}
}
