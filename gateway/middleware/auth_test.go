package middleware

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const authTestSecret = "test-secret"
const authTestConfig = "jwt:\n  secret: " + authTestSecret + "\n"

// makeToken 用指定密钥和过期时间签发一个 HS256 Token。
func makeToken(t *testing.T, secret string, exp time.Time) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user-1",
		"exp": exp.Unix(),
	})
	ss, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return ss
}

// doAuth 用 AuthMiddleware 处理一个带 Authorization 头的请求。
func doAuth(t *testing.T, authHeader, path string) *httptest.ResponseRecorder {
	t.Helper()
	h := AuthMiddleware(http.HandlerFunc(okHandler))
	headers := map[string]string{}
	if authHeader != "" {
		headers["Authorization"] = authHeader
	}
	return doReq(h, "GET", path, headers)
}

// TestAuthValidToken 验证合法 Token 放行，且 "Bearer x" 与裸 Token 两种写法都支持。
func TestAuthValidToken(t *testing.T) {
	setTestConfig(t, authTestConfig)
	valid := makeToken(t, authTestSecret, time.Now().Add(time.Hour))

	for _, hdr := range []string{"Bearer " + valid, valid} {
		rec := doAuth(t, hdr, "/api/x")
		if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
			t.Fatalf("header %q: code=%d body=%q, want 200/ok", hdr[:12], rec.Code, rec.Body.String())
		}
	}
}

// TestAuthMissingToken 验证缺失 Token 返回 401 JSON。
func TestAuthMissingToken(t *testing.T) {
	setTestConfig(t, authTestConfig)
	rec := doAuth(t, "", "/api/x")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "missing authorization token") {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want json", ct)
	}
}

// TestAuthInvalidToken 验证各类非法 Token 均被拒绝：
// 乱码、错误密钥签发、已过期。
func TestAuthInvalidToken(t *testing.T) {
	setTestConfig(t, authTestConfig)
	cases := map[string]string{
		"garbage":      "Bearer bad.token.here",
		"wrong-secret": "Bearer " + makeToken(t, "other-secret", time.Now().Add(time.Hour)),
		"expired":      "Bearer " + makeToken(t, authTestSecret, time.Now().Add(-time.Hour)),
	}
	for name, hdr := range cases {
		if rec := doAuth(t, hdr, "/api/x"); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: code = %d, want 401", name, rec.Code)
		}
	}
}

// TestAuthRejectsAlgNone 验证 "alg=none" 伪造 Token 被拒绝（算法混淆攻击回归测试）。
func TestAuthRejectsAlgNone(t *testing.T) {
	setTestConfig(t, authTestConfig)
	// 手工构造无签名 Token：header 声明 alg=none，签名段为空
	h := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	p := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"hacker"}`))
	rec := doAuth(t, "Bearer "+h+"."+p+".", "/api/x")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("alg=none token: code = %d, want 401", rec.Code)
	}
}

// TestAuthExcludedPaths 验证白名单路径（/swagger/）免认证，其余路径仍需 Token。
func TestAuthExcludedPaths(t *testing.T) {
	setTestConfig(t, authTestConfig)
	if rec := doAuth(t, "", "/swagger/index.html"); rec.Code != http.StatusOK {
		t.Fatalf("swagger path: code = %d, want 200 (excluded from auth)", rec.Code)
	}
	if rec := doAuth(t, "", "/api/x"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("api path: code = %d, want 401", rec.Code)
	}
}
