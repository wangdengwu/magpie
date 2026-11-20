package teamd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"
)

// Server is teamd: the portal's HTTP surface, over teamd's own files and
// the one client that reaches magpie.
type Server struct {
	cfg   Config
	store *Store
	admin *AdminClient
	// signIns are the sign-ins this portal started, by the team that
	// started them (signin.go). In memory: a restart loses the links it
	// handed out, never an account already signed in.
	signIns *signIns
	// groupMu serialises the read-modify-write of an account group. magpie
	// replaces the whole list on a save, so two adds at once would lose
	// one of them.
	groupMu sync.Mutex
	// now is the clock, for the sign-ins' expiry and the pages' ages.
	now func() time.Time
	// icon and iconDark are the mark beside each page's head, on a light and
	// a dark ground, and favicon the tab's icon: each a data: URL, "" for
	// none (brand.go).
	icon     string
	iconDark string
	favicon  string
}

// New opens the state folder and reads the admin key. Everything that can
// be wrong with the configuration is refused here, at start, rather than
// on a page nobody is watching.
func New(cfg Config) (*Server, error) {
	store, admin, err := open(cfg)
	if err != nil {
		return nil, err
	}
	icon, err := loadIcon(cfg.IconFile)
	if err != nil {
		return nil, err
	}
	favicon, err := loadIcon(cfg.FaviconFile)
	if err != nil {
		return nil, err
	}
	iconDark, err := loadIcon(cfg.IconDarkFile)
	if err != nil {
		return nil, err
	}
	if favicon == "" {
		favicon = icon
	}
	return &Server{cfg: cfg, store: store, admin: admin, signIns: newSignIns(), now: time.Now, icon: icon, iconDark: iconDark, favicon: favicon}, nil
}

// open checks cfg and opens what the portal and the ops commands both use.
func open(cfg Config) (*Store, *AdminClient, error) {
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	store, err := NewStore(cfg.StateDir, cfg.AuditMax)
	if err != nil {
		return nil, nil, err
	}
	admin, err := NewAdminClient(cfg)
	if err != nil {
		return nil, nil, err
	}
	return store, admin, nil
}

// Store is where this server keeps its teams, for the ops commands.
func (s *Server) Store() *Store { return s.store }

// Handler is the portal's routes. Every page and every action starts at
// Authorize: the pages through authorizePage, the add flow's JSON routes
// through pageGrant.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /team/{token}", s.serveAccounts)
	s.addRoutes(mux)
	s.switchRoutes(mux)
	s.usageRoutes(mux)
	s.connectRoutes(mux)
	s.auditRoutes(mux)
	s.membersRoutes(mux)
	return portalHeaders(http.MaxBytesHandler(mux, maxForm))
}

// maxForm is the most a request may send. The portal's largest form is a
// pasted callback URL, a few KiB; Go's own cap, 10 MiB a form, would let a
// few dozen posts at once take the sidecar out of memory, and a sidecar
// that dies takes the Pod's readiness, and so the gateway, with it.
const maxForm = 64 << 10

// health says whether teamd can work: its own teams read, and whether
// magpie answers. magpie being down is not this container's fault and does
// not fail the check — a sidecar restarting with it would restart nothing
// useful — so it is said, not counted.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	teams, err := s.store.Teams()
	if err != nil {
		// The boundary refuses every page when its teams can't be read, so
		// this is the one state in which teamd is not up.
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	magpie := "ok"
	if _, err := s.admin.AccountGroups(ctx); err != nil {
		magpie = "unreachable"
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "teams": len(teams), "magpie": magpie})
}

// Serve answers on the configured address until ctx is done.
func (s *Server) Serve(ctx context.Context) error {
	go s.signInJanitor(ctx)
	srv := &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	failed := make(chan error, 1)
	go func() { failed <- srv.ListenAndServe() }()
	select {
	case err := <-failed:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}
	// In-flight requests get long enough to finish: a page that was being
	// answered when the Pod was told to stop is not cut in half.
	stop, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(stop); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

// portalHeaders are on every answer: the token in a page's path is a
// secret, so nothing here is cacheable, no referrer goes out, and no
// browser guesses at the type of what it got.
func portalHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
