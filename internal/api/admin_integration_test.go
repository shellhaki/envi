package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/access"
	"shellhaki/envi/internal/admin"
	"shellhaki/envi/internal/announce"
	"shellhaki/envi/internal/audit"
	"shellhaki/envi/internal/auth"
	"shellhaki/envi/internal/billing"
	"shellhaki/envi/internal/config"
	crypt "shellhaki/envi/internal/crypto"
	"shellhaki/envi/internal/invitation"
	"shellhaki/envi/internal/kv"
	"shellhaki/envi/internal/project"
	"shellhaki/envi/internal/secret"
	"shellhaki/envi/internal/service_token"
	"shellhaki/envi/internal/workspace"
)

// adminFixture builds a real router over a real database with two signed-in
// people: one whose address is in ENVI_ADMINS and one whose is not.
//
// The second is the one that matters. Checking that a stranger is refused
// proves almost nothing — RequireAuth already refuses them. The question an
// operator endpoint has to answer is whether an ordinary signed-in user, with
// a perfectly valid session, can read the whole instance.
type adminFixture struct {
	router                   *gin.Engine
	operatorTok, ordinaryTok string
	ordinaryEmail            string
}

func newAdminFixture(t *testing.T) *adminFixture {
	t.Helper()
	if os.Getenv("ENVI_INTEGRATION") != "1" {
		t.Skip("set ENVI_INTEGRATION=1")
	}
	db, err := pgxpool.New(t.Context(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)

	suffix := time.Now().UnixNano()
	operatorEmail := fmt.Sprintf("admin-op-%d@example.test", suffix)
	ordinaryEmail := fmt.Sprintf("admin-user-%d@example.test", suffix)

	// Set before the router is built: operators() is read once, when
	// RequireOperator is constructed.
	t.Setenv("ENVI_ADMINS", operatorEmail)

	sessionFor := func(email string) string {
		w, err := workspace.Service{DB: db}.Provision(t.Context(), email)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Exec(t.Context(), `DELETE FROM users WHERE id=$1`, w.UserID) })
		access, _, err := auth.StartSession(db, w.UserID)
		if err != nil {
			t.Fatal(err)
		}
		return access
	}
	operatorTok := sessionFor(operatorEmail)
	ordinaryTok := sessionFor(ordinaryEmail)

	cipher, err := crypt.New([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	router := Build(db,
		auth.LoginSettings{},
		project.Service{DB: db},
		secret.Service{DB: db, Access: access.Service{DB: db}, Cipher: cipher},
		kv.Store{DB: db, Cipher: cipher},
		audit.Service{DB: db},
		service_token.Service{DB: db},
		invitation.Service{DB: db},
		"http://web.test",
		false,
		config.DefaultDeployment(),
		announce.Service{Admin: admin.Service{DB: db}},
		billing.Service{DB: db},
		"",
	)
	return &adminFixture{router: router, operatorTok: operatorTok, ordinaryTok: ordinaryTok, ordinaryEmail: ordinaryEmail}
}

func (f *adminFixture) get(t *testing.T, path, bearer string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

// An operator reads the instance.
func TestOperatorReadsStats(t *testing.T) {
	f := newAdminFixture(t)

	status, body := f.get(t, "/admin/stats", f.operatorTok)
	if status != 200 {
		t.Fatalf("operator got %d: %s", status, body)
	}

	var snapshot struct {
		Totals struct {
			Users    int64 `json:"users"`
			Projects int64 `json:"projects"`
		} `json:"totals"`
		Revenue struct {
			Available bool `json:"available"`
		} `json:"revenue"`
		GeneratedAt string `json:"generated_at"`
	}
	if err := json.Unmarshal(body, &snapshot); err != nil {
		t.Fatal(err)
	}
	// The fixture just made two users, so the instance has at least two.
	if snapshot.Totals.Users < 2 {
		t.Errorf("users = %d, want at least the 2 this test created", snapshot.Totals.Users)
	}
	if snapshot.GeneratedAt == "" {
		t.Error("no generated_at in the reply")
	}
}

// An ordinary signed-in user is refused, and is told nothing about what they
// were refused from. This is the test the endpoint exists to pass.
func TestOrdinaryUserCannotReadStats(t *testing.T) {
	f := newAdminFixture(t)

	status, body := f.get(t, "/admin/stats", f.ordinaryTok)
	if status != 404 {
		t.Fatalf("ordinary user got %d, want 404: %s", status, body)
	}
	// A 403 would confirm the endpoint is real. A body would too.
	if len(body) != 0 {
		t.Errorf("refusal carried a body, which tells a caller the route exists: %s", body)
	}
}

// Same for whoami, which the dashboard uses to decide whether to show a link.
func TestWhoamiFollowsTheSameRule(t *testing.T) {
	f := newAdminFixture(t)

	if status, body := f.get(t, "/admin/whoami", f.operatorTok); status != 200 {
		t.Errorf("operator whoami got %d: %s", status, body)
	}
	if status, _ := f.get(t, "/admin/whoami", f.ordinaryTok); status != 404 {
		t.Errorf("ordinary whoami got %d, want 404", status)
	}
}

// No session at all stops at RequireAuth, before the operator check runs.
func TestUnauthenticatedStats(t *testing.T) {
	f := newAdminFixture(t)
	if status, _ := f.get(t, "/admin/stats", ""); status != 401 {
		t.Errorf("unauthenticated got %d, want 401", status)
	}
}

// An announcer may announce and nothing else.
//
// This is the test the two roles exist to pass. An announcer is on the
// operator screen to write to customers, not to read the instance's revenue,
// its expenses, or the list of who else may act on it.
func TestAnnouncerCannotReachTheFigures(t *testing.T) {
	f := newAdminFixture(t)

	db, err := pgxpool.New(t.Context(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)

	// Appoint the ordinary user as an announcer.
	email := f.ordinaryEmail
	operators := admin.Service{DB: db}
	if _, err := operators.AddOperator(t.Context(), "", email, "Test Announcer", admin.RoleAnnouncer); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(context.Background(), `DELETE FROM operators WHERE email = $1`, email) })

	// Allowed.
	for _, path := range []string{"/admin/whoami", "/admin/announcements"} {
		if status, body := f.get(t, path, f.ordinaryTok); status != 200 {
			t.Errorf("announcer got %d for %s, want 200: %s", status, path, body)
		}
	}

	// Refused, and told nothing about what they were refused from.
	for _, path := range []string{"/admin/stats", "/admin/operators"} {
		status, body := f.get(t, path, f.ordinaryTok)
		if status != 404 {
			t.Errorf("announcer got %d for %s, want 404: %s", status, path, body)
		}
		if len(body) != 0 {
			t.Errorf("%s refusal carried a body: %s", path, body)
		}
	}

	// The admin still reaches everything.
	for _, path := range []string{"/admin/stats", "/admin/operators", "/admin/announcements"} {
		if status, body := f.get(t, path, f.operatorTok); status != 200 {
			t.Errorf("admin got %d for %s, want 200: %s", status, path, body)
		}
	}
}

// whoami reports the role, which is what the dashboard uses to decide which
// panels to render.
func TestWhoamiReportsTheRole(t *testing.T) {
	f := newAdminFixture(t)

	_, body := f.get(t, "/admin/whoami", f.operatorTok)
	var who struct {
		Role     string `json:"role"`
		Announce bool   `json:"announce"`
		Manage   bool   `json:"manage"`
	}
	if err := json.Unmarshal(body, &who); err != nil {
		t.Fatal(err)
	}
	if who.Role != admin.RoleAdmin || !who.Announce || !who.Manage {
		t.Fatalf("admin whoami = %+v", who)
	}
}
