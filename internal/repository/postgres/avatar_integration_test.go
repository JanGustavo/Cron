package postgres_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/JanGustavo/Cron/internal/api/handler"
	"github.com/JanGustavo/Cron/internal/api/middleware"
	"github.com/JanGustavo/Cron/internal/auth"
	"github.com/JanGustavo/Cron/internal/config"
	"github.com/JanGustavo/Cron/internal/repository/postgres"
	"github.com/JanGustavo/Cron/internal/service"
)

func TestAvatarProfileIntegration(t *testing.T) {
	dsn := os.Getenv("CRONFLOW_MONITOR_TEST_DSN")
	if dsn == "" {
		t.Skip("requires disposable migrated PostgreSQL")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	repo := postgres.NewUserRepository(db)
	u, err := repo.CreateUser(ctx, "avatar-test@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, u.ID)
	proj, err := repo.CreateProject(ctx, u.ID, "Avatar test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindUserByProjectID(ctx, proj.ID); err != nil {
		t.Fatal(err)
	}
	const secret = "avatar-integration-secret"
	token, err := auth.GenerateToken(u.ID, u.Email, proj.ID, "free", secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	h := handler.NewAuthHandler(repo, nil, service.NewEntitlementEngine(postgres.NewBillingRepository(db)), &config.Config{JWTSecret: secret})
	call := func(method, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/v1/users/profile", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		target := http.HandlerFunc(h.UpdateProfile)
		if method == "GET" {
			target = h.GetProfile
		}
		middleware.Auth(repo, secret)(target).ServeHTTP(rec, req)
		return rec
	}
	cases := []struct {
		name, field, want string
		code              int
	}{
		{"existing account fallback", "", "", 200},
		{"set URL", `,"avatar_url":"https://example.com/photo.jpg"`, "https://example.com/photo.jpg", 200},
		{"old client preserves URL", "", "https://example.com/photo.jpg", 200},
		{"null preserves URL", `,"avatar_url":null`, "https://example.com/photo.jpg", 200},
		{"trim and replace", `,"avatar_url":" https://example.com/new.png "`, "https://example.com/new.png", 200},
		{"reject insecure URL", `,"avatar_url":"http://example.com/photo.jpg"`, "https://example.com/new.png", 400},
		{"reject credentials", `,"avatar_url":"https://u:p@example.com/photo.jpg"`, "https://example.com/new.png", 400},
		{"remove URL", `,"avatar_url":""`, "", 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"full_name":"Avatar Test","timezone":"UTC","digest_hour":18` + tc.field + `}`
			rec := call("PUT", body)
			if rec.Code != tc.code {
				t.Fatalf("PUT: %d %s", rec.Code, rec.Body.String())
			}
			rec = call("GET", "")
			if rec.Code != 200 {
				t.Fatalf("GET: %d %s", rec.Code, rec.Body.String())
			}
			var p handler.ProfileResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || p.AvatarURL != tc.want {
				t.Fatalf("profile URL %q expected %q: %v", p.AvatarURL, tc.want, err)
			}
			found, err := repo.FindByEmail(ctx, u.Email)
			if err != nil || found.AvatarURL != tc.want {
				t.Fatalf("login read mismatch: %+v %v", found, err)
			}
		})
	}
}
