package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"shellhaki/envi/internal/apikey"
	"shellhaki/envi/internal/auth"
	"shellhaki/envi/internal/service_token"
)

func (f *configFixture) kv(t *testing.T, method, query, bearer string, body any) (int, map[string]any) {
	t.Helper()
	var payload []byte
	if body != nil {
		payload, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, "/kv"+query, bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

// The store is project-wide and separate from secrets: a value set here must
// not appear among the project's secrets, in any environment.
func TestKVIsSeparateFromSecrets(t *testing.T) {
	f := newConfigFixture(t)
	session, _, err := auth.StartSession(f.db, f.userID)
	if err != nil {
		t.Fatal(err)
	}

	if code, body := f.kv(t, http.MethodPut, "", session,
		map[string]string{"project": f.projectName, "key": "THEME", "value": "dark"}); code != 200 {
		t.Fatalf("set: %d %v", code, body)
	}
	code, body := f.kv(t, http.MethodGet, "?project="+f.projectName, session, nil)
	if code != 200 {
		t.Fatalf("list: %d %v", code, body)
	}
	values, _ := body["values"].(map[string]any)
	if values["THEME"] != "dark" {
		t.Fatalf("stored value came back as %v", values["THEME"])
	}

	// The secrets of both environments must be untouched by that write.
	for _, env := range []string{"development", "production"} {
		_, secrets := f.get(t, "?project="+f.projectName+"&environment="+env, session)
		if _, leaked := secrets.Values["THEME"]; leaked {
			t.Fatalf("a kv entry leaked into %s's secrets", env)
		}
	}
}

func TestKVSetGetDelete(t *testing.T) {
	f := newConfigFixture(t)
	session, _, err := auth.StartSession(f.db, f.userID)
	if err != nil {
		t.Fatal(err)
	}
	set := func(k, v string) (int, map[string]any) {
		return f.kv(t, http.MethodPut, "", session, map[string]string{"project": f.projectName, "key": k, "value": v})
	}

	set("A", "1")
	set("B", "2")
	set("A", "overwritten") // a second write replaces, it does not duplicate

	_, body := f.kv(t, http.MethodGet, "?project="+f.projectName, session, nil)
	values, _ := body["values"].(map[string]any)
	if values["A"] != "overwritten" || values["B"] != "2" || len(values) != 2 {
		t.Fatalf("after writes: %v", values)
	}

	if code, _ := f.kv(t, http.MethodDelete, "", session, map[string]string{"project": f.projectName, "key": "A"}); code != 204 {
		t.Fatalf("delete returned %d", code)
	}
	_, body = f.kv(t, http.MethodGet, "?project="+f.projectName, session, nil)
	values, _ = body["values"].(map[string]any)
	if _, still := values["A"]; still {
		t.Fatal("deleted key came back")
	}

	// Deleting something absent is not an error: the caller wanted it gone.
	if code, _ := f.kv(t, http.MethodDelete, "", session, map[string]string{"project": f.projectName, "key": "never"}); code != 204 {
		t.Fatal("deleting an absent key was an error")
	}
	// An empty key is, though.
	if code, _ := set("", "x"); code != 400 {
		t.Fatalf("empty key returned %d, want 400", code)
	}
}

// A service token belongs to an environment; the store belongs to the project
// that environment is in. A read-only token must not be able to write.
func TestKVWithServiceToken(t *testing.T) {
	f := newConfigFixture(t)
	projectID := projectOf(t, f)

	readOnly, err := service_token.Service{DB: f.db}.Create(t.Context(), f.userID, projectID, f.devEnvID, "sdk-read", "read", 0)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := service_token.Service{DB: f.db}.Create(t.Context(), f.userID, projectID, f.devEnvID, "sdk-write", "write", 0)
	if err != nil {
		t.Fatal(err)
	}

	if code, body := f.kv(t, http.MethodPut, "", writer.Value, map[string]string{"key": "FROM", "value": "token"}); code != 200 {
		t.Fatalf("write token could not set: %d %v", code, body)
	}
	code, body := f.kv(t, http.MethodGet, "", readOnly.Value, nil)
	if code != 200 || body["project"] != f.projectName {
		t.Fatalf("read token: %d %v", code, body)
	}
	if values, _ := body["values"].(map[string]any); values["FROM"] != "token" {
		t.Fatalf("read token saw %v", body["values"])
	}
	if code, _ := f.kv(t, http.MethodPut, "", readOnly.Value, map[string]string{"key": "FROM", "value": "nope"}); code != 403 {
		t.Fatalf("read-only token wrote anyway: %d", code)
	}
	// Naming someone else's project fails loudly rather than being redirected.
	if code, _ := f.kv(t, http.MethodGet, "?project=some-other-project", readOnly.Value, nil); code != 403 {
		t.Fatalf("token reached past its own project: %d", code)
	}
}

// An API key's ceiling applies here too: a read-only key may not write.
func TestKVWithReadOnlyAPIKey(t *testing.T) {
	f := newConfigFixture(t)
	key, err := apikey.Create(t.Context(), f.db, f.userID, "sdk", "read", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	session, _, err := auth.StartLimitedSession(f.db, f.userID, key.Permission, key.ID)
	if err != nil {
		t.Fatal(err)
	}

	if code, _ := f.kv(t, http.MethodGet, "?project="+f.projectName, session, nil); code != 200 {
		t.Fatal("read-only key could not read")
	}
	if code, _ := f.kv(t, http.MethodPut, "", session,
		map[string]string{"project": f.projectName, "key": "X", "value": "1"}); code != 403 {
		t.Fatalf("read-only key wrote: %d", code)
	}
}
