package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/skrisharam-web/live-polling/backend/internal/config"
	"github.com/skrisharam-web/live-polling/backend/internal/handlers"
	"github.com/skrisharam-web/live-polling/backend/internal/middleware"
	"github.com/skrisharam-web/live-polling/backend/internal/repositories"
	"github.com/skrisharam-web/live-polling/backend/internal/router"
	"github.com/skrisharam-web/live-polling/backend/internal/services"
)

// testAPI drives the real router — the same middleware chain, handlers, services
// and repositories the server runs — against a real MongoDB. Only the network is
// replaced, by httptest, so what these tests verify is what production does.
type testAPI struct {
	t      *testing.T
	engine *gin.Engine
	// cookies is a tiny cookie jar, so a test can behave like a browser and carry
	// its session from one request to the next.
	cookies map[string]string
}

func newTestAPI(t *testing.T) *testAPI {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db := newTestDB(t)

	cfg := &config.Config{
		AppEnv:              config.EnvTest,
		Port:                "0",
		JWTSecret:           "integration-test-secret-that-is-long-enough",
		JWTExpiresIn:        time.Hour,
		AllowedOrigins:      []string{"http://localhost:5173"},
		CookieSecure:        false,
		MaxRequestBodyBytes: 64 * 1024,
	}

	userRepo := repositories.NewUserRepository(db)
	pollRepo := repositories.NewPollRepository(db)
	voteRepo := repositories.NewVoteRepository(db)

	authService := services.NewAuthService(userRepo, cfg.JWTSecret, cfg.JWTExpiresIn)
	pollService := services.NewPollService(pollRepo, voteRepo, nil)
	cookies := handlers.NewCookieSettings(cfg)

	engine := router.New(cfg, router.Dependencies{
		Health:       handlers.NewHealthHandler(db, nil),
		Auth:         handlers.NewAuthHandler(authService, cookies),
		Poll:         handlers.NewPollHandler(pollService),
		UserResolver: authService,
	})

	return &testAPI{t: t, engine: engine, cookies: make(map[string]string)}
}

// apiResponse mirrors the envelope every endpoint returns.
type apiResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Fields  map[string]string `json:"fields"`
	} `json:"error"`
}

func (a *testAPI) do(method, path string, body any, opts ...func(*http.Request)) (*httptest.ResponseRecorder, apiResponse) {
	a.t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			a.t.Fatalf("encode request body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, value := range a.cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	for _, opt := range opts {
		opt(req)
	}

	rec := httptest.NewRecorder()
	a.engine.ServeHTTP(rec, req)

	// Record Set-Cookie just as a browser would, including deletions.
	for _, cookie := range rec.Result().Cookies() {
		if cookie.MaxAge < 0 || cookie.Value == "" {
			delete(a.cookies, cookie.Name)
			continue
		}
		a.cookies[cookie.Name] = cookie.Value
	}

	var parsed apiResponse
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
			a.t.Fatalf("response for %s %s is not the standard envelope: %s", method, path, rec.Body.String())
		}
	}
	return rec, parsed
}

func (a *testAPI) register(name, email, password string) (*httptest.ResponseRecorder, apiResponse) {
	a.t.Helper()
	return a.do(http.MethodPost, "/api/auth/register", map[string]string{
		"name": name, "email": email, "password": password,
	})
}

func (a *testAPI) logout() {
	a.t.Helper()
	a.do(http.MethodPost, "/api/auth/logout", map[string]string{})
}

func assertStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d (body: %s)", rec.Code, want, rec.Body.String())
	}
}

func assertErrorCode(t *testing.T, res apiResponse, want string) {
	t.Helper()
	if res.Error == nil {
		t.Fatalf("expected an error payload, got %s", string(res.Data))
	}
	if res.Error.Code != want {
		t.Errorf("error code = %q, want %q", res.Error.Code, want)
	}
}

func TestRegisterLoginLogoutFlow(t *testing.T) {
	api := newTestAPI(t)

	t.Run("register issues a session", func(t *testing.T) {
		rec, res := api.register("Ada Lovelace", "Ada@Example.com", "correct-horse-battery")
		assertStatus(t, rec, http.StatusCreated)

		if _, ok := api.cookies[middleware.SessionCookieName]; !ok {
			t.Fatal("no session cookie was set")
		}

		var payload struct {
			User struct {
				ID    string `json:"id"`
				Email string `json:"email"`
			} `json:"user"`
		}
		if err := json.Unmarshal(res.Data, &payload); err != nil {
			t.Fatalf("decode data: %v", err)
		}
		if payload.User.Email != "ada@example.com" {
			t.Errorf("email = %q, want the normalised form", payload.User.Email)
		}
		if payload.User.ID == "" {
			t.Error("no user id was returned")
		}
	})

	t.Run("the session cookie is HttpOnly and scoped", func(t *testing.T) {
		rec, _ := api.do(http.MethodPost, "/api/auth/login", map[string]string{
			"email": "ada@example.com", "password": "correct-horse-battery",
		})
		assertStatus(t, rec, http.StatusOK)

		var session *http.Cookie
		for _, cookie := range rec.Result().Cookies() {
			if cookie.Name == middleware.SessionCookieName {
				session = cookie
			}
		}
		if session == nil {
			t.Fatal("login set no session cookie")
		}
		if !session.HttpOnly {
			t.Error("the session cookie must be HttpOnly so page scripts cannot read it")
		}
		if session.Path != "/" {
			t.Errorf("cookie path = %q, want /", session.Path)
		}
		if session.MaxAge <= 0 {
			t.Errorf("cookie MaxAge = %d, want the token lifetime", session.MaxAge)
		}
	})

	t.Run("the response never contains the token or a password field", func(t *testing.T) {
		rec, _ := api.do(http.MethodGet, "/api/auth/me", nil)
		assertStatus(t, rec, http.StatusOK)

		body := rec.Body.String()
		for _, forbidden := range []string{"password", "passwordHash", "token", api.cookies[middleware.SessionCookieName]} {
			if forbidden != "" && strings.Contains(body, forbidden) {
				t.Errorf("the response body leaks %q: %s", forbidden, body)
			}
		}
	})

	t.Run("logout clears the session", func(t *testing.T) {
		api.logout()
		if _, ok := api.cookies[middleware.SessionCookieName]; ok {
			t.Fatal("the session cookie survived logout")
		}

		rec, res := api.do(http.MethodGet, "/api/auth/me", nil)
		assertStatus(t, rec, http.StatusUnauthorized)
		assertErrorCode(t, res, "UNAUTHORIZED")
	})
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	api := newTestAPI(t)

	rec, _ := api.register("Ada", "ada@example.com", "correct-horse-battery")
	assertStatus(t, rec, http.StatusCreated)

	rec, res := api.register("Imposter", "ADA@example.com", "different-password")
	assertStatus(t, rec, http.StatusConflict)
	assertErrorCode(t, res, "CONFLICT")
}

func TestRegisterValidationReportsEveryField(t *testing.T) {
	api := newTestAPI(t)

	rec, res := api.register("A", "not-an-email", "short")
	assertStatus(t, rec, http.StatusUnprocessableEntity)
	assertErrorCode(t, res, "VALIDATION_ERROR")

	for _, field := range []string{"name", "email", "password"} {
		if _, ok := res.Error.Fields[field]; !ok {
			t.Errorf("fields = %v, want an entry for %q", res.Error.Fields, field)
		}
	}
}

func TestLoginFailuresAreIndistinguishable(t *testing.T) {
	api := newTestAPI(t)
	if rec, _ := api.register("Ada", "ada@example.com", "correct-horse-battery"); rec.Code != http.StatusCreated {
		t.Fatalf("setup registration failed: %d", rec.Code)
	}
	api.logout()

	cases := []struct {
		name  string
		email string
		pass  string
	}{
		{"wrong password", "ada@example.com", "wrong-password"},
		{"unknown account", "nobody@example.com", "correct-horse-battery"},
	}

	var seen []string
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, res := api.do(http.MethodPost, "/api/auth/login", map[string]string{"email": tc.email, "password": tc.pass})
			assertStatus(t, rec, http.StatusUnauthorized)
			assertErrorCode(t, res, "UNAUTHORIZED")
			seen = append(seen, res.Error.Message)
		})
	}

	if len(seen) == 2 && seen[0] != seen[1] {
		t.Errorf("a wrong password and an unknown account returned different messages (%q vs %q), which lets accounts be enumerated", seen[0], seen[1])
	}
}

func TestProtectedRouteRejectsTamperedSession(t *testing.T) {
	api := newTestAPI(t)
	if rec, _ := api.register("Ada", "ada@example.com", "correct-horse-battery"); rec.Code != http.StatusCreated {
		t.Fatalf("setup registration failed: %d", rec.Code)
	}

	original := api.cookies[middleware.SessionCookieName]

	cases := map[string]string{
		"empty":             "",
		"garbage":           "not-a-jwt",
		"flipped signature": original[:len(original)-3] + "aaa",
		"truncated":         original[:len(original)/2],
	}

	for name, token := range cases {
		t.Run(name+" is rejected", func(t *testing.T) {
			api.cookies[middleware.SessionCookieName] = token
			rec, res := api.do(http.MethodGet, "/api/auth/me", nil)
			assertStatus(t, rec, http.StatusUnauthorized)
			assertErrorCode(t, res, "UNAUTHORIZED")
		})
	}
}

func TestStateChangingRequestsRequireJSONContentType(t *testing.T) {
	api := newTestAPI(t)

	// A browser can only send a cross-site form as one of three content types, and
	// none of them is application/json. Refusing those is what stops a malicious
	// page from driving this API with the user's cookie attached.
	for _, contentType := range []string{"application/x-www-form-urlencoded", "multipart/form-data", "text/plain"} {
		t.Run(contentType+" is refused", func(t *testing.T) {
			rec, res := api.do(http.MethodPost, "/api/auth/login", map[string]string{
				"email": "ada@example.com", "password": "correct-horse-battery",
			}, func(req *http.Request) {
				req.Header.Set("Content-Type", contentType)
			})
			assertStatus(t, rec, http.StatusUnprocessableEntity)
			assertErrorCode(t, res, "VALIDATION_ERROR")
		})
	}
}

func TestCORSOnlyAnswersAllowedOrigins(t *testing.T) {
	api := newTestAPI(t)

	t.Run("an allowed origin gets credentialed CORS headers", func(t *testing.T) {
		rec, _ := api.do(http.MethodOptions, "/api/auth/login", nil, func(req *http.Request) {
			req.Header.Set("Origin", "http://localhost:5173")
			req.Header.Set("Access-Control-Request-Method", "POST")
		})
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
			t.Errorf("Allow-Origin = %q, want the requesting origin", got)
		}
		if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Errorf("Allow-Credentials = %q, want true", got)
		}
	})

	t.Run("an unknown origin gets no CORS headers at all", func(t *testing.T) {
		rec, _ := api.do(http.MethodOptions, "/api/auth/login", nil, func(req *http.Request) {
			req.Header.Set("Origin", "https://evil.example.com")
			req.Header.Set("Access-Control-Request-Method", "POST")
		})
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Allow-Origin = %q, want empty so the browser blocks the response", got)
		}
	})
}

func TestOversizedBodyIsRejected(t *testing.T) {
	api := newTestAPI(t)

	rec, res := api.register("Ada", "ada@example.com", strings.Repeat("x", 100_000))
	assertStatus(t, rec, http.StatusRequestEntityTooLarge)
	assertErrorCode(t, res, "PAYLOAD_TOO_LARGE")
}
