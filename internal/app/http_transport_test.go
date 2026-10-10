package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExplicitLocalHTTPPreservesAuthenticationOriginAndCSRF(t *testing.T) {
	db := authDB(t)
	origin := "http://localhost:8080"
	call := func(h http.Handler, path, requestOrigin string, cookie *http.Cookie, csrf string, body any) *httptest.ResponseRecorder {
		var b bytes.Buffer
		if body != nil {
			json.NewEncoder(&b).Encode(body)
		}
		r := httptest.NewRequest("POST", origin+path, &b)
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("Origin", requestOrigin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	credentials := map[string]string{"username": "viewer", "password": testPassword}
	requireStatus(t, call(HandlerWithOrigin(db, origin), "/api/v1/auth/login", origin, nil, "", credentials), 400)
	h := HandlerWithTransport(db, origin, true)
	requireStatus(t, call(h, "/api/v1/auth/login", "http://other.invalid", nil, "", credentials), 403)
	w := call(h, "/api/v1/auth/login", origin, nil, "", credentials)
	requireStatus(t, w, 200)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("session cookie missing")
	}
	c := cookies[0]
	if c.Name != "drk-local-session" || c.Secure || !c.HttpOnly || c.Path != "/" || c.Domain != "" || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("local cookie policy: %+v", c)
	}
	var response map[string]string
	json.Unmarshal(w.Body.Bytes(), &response)
	requireStatus(t, call(h, "/api/v1/auth/logout", origin, c, "", nil), 403)
	requireStatus(t, call(h, "/api/v1/auth/logout", "http://other.invalid", c, response["csrf_token"], nil), 403)
	requireStatus(t, call(h, "/api/v1/auth/logout", origin, c, response["csrf_token"], nil), 204)
}
