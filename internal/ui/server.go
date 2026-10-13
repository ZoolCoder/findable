// Package ui is the local admin page, drawn inside toolshell. It reaches live
// state only through the engine, the same path the CLI takes, and keeps no
// state beyond the last plan it ran per target. The command that starts it
// wraps it in toolshell/guard: loopback only, Host check, no cross-site
// writes, capped bodies.
package ui

import (
	"context"
	"embed"
	"html/template"
	"net/http"
	"os/user"
	"sync"
	"time"

	"github.com/zoolcoder/findable/internal/config"
	"github.com/zoolcoder/findable/internal/engine"
	"github.com/zoolcoder/findable/internal/plan"
	"github.com/zoolcoder/toolshell"
	"github.com/zoolcoder/toolshell/activity"
)

//go:embed templates/*.html
var assets embed.FS

const activityLimit = 200

// Deps is everything the page needs from the command that starts it.
type Deps struct {
	// Name is the binary name, shown in commands the page tells you to run.
	Name string
	// ConfigPath is re-read on every request, so an edit shows without a
	// restart.
	ConfigPath string
	// Activity records what the page did; nil records nothing.
	Activity *activity.Log
	// Providers is what the engine plans with.
	Providers engine.Providers
	// Host is the address the page is served on, shown in the sidebar.
	Host string
	// Now is swapped in tests.
	Now func() time.Time
}

type Server struct {
	deps  Deps
	shell *toolshell.Shell
	tmpl  *template.Template
	mux   *http.ServeMux

	mu   sync.Mutex
	runs map[string]run
}

// run is the last plan or apply the page made for one target.
type run struct {
	At      time.Time
	Plan    plan.Plan
	Applied bool
	Output  string
	Err     string
}

// New wires the routes.
func New(deps Deps) (*Server, error) {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	s := &Server{deps: deps, mux: http.NewServeMux(), runs: map[string]run{}}
	s.shell = toolshell.New(toolshell.Config{
		Brand:  toolshell.Brand{Name: deps.Name, Tagline: "local admin"},
		Status: toolshell.Status{Label: "Local only", Detail: deps.Host},
		Cookie: deps.Name,
		User:   toolshell.User{Name: localUser(), Detail: deps.Host},
		Menu: []toolshell.NavItem{
			{Href: "/settings", Label: "Settings", Icon: "gear", Hotkey: "s"},
		},
	})
	tmpl, err := s.shell.Templates(assets, "templates/*.html", template.FuncMap{
		"opChip": opChip,
		"stamp":  func(at time.Time) string { return at.Local().Format("2006-01-02 15:04:05") },
	})
	if err != nil {
		return nil, err
	}
	s.tmpl = tmpl
	s.shell.Mount(s.mux)

	s.mux.HandleFunc("GET /{$}", s.targets)
	s.mux.HandleFunc("GET /targets/{name}", s.target)
	s.mux.HandleFunc("POST /targets/{name}/plan", s.plan)
	s.mux.HandleFunc("POST /targets/{name}/apply", s.apply)
	s.mux.HandleFunc("GET /activity", s.activity)
	s.mux.HandleFunc("GET /settings", s.settings)
	s.mux.HandleFunc("GET /", s.notFound)
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

type chrome = toolshell.Chrome

func (s *Server) chrome(r *http.Request, active string) chrome {
	n := 0
	if cfg, err := s.config(); err == nil {
		n = len(cfg.Targets)
	}
	c := s.shell.Chrome(r, active, []toolshell.NavGroup{
		{Items: []toolshell.NavItem{
			{Href: "/", Label: "Targets", Key: "targets", Icon: "grid", Count: n, Hotkey: "h"},
		}},
		{Title: "System", Items: []toolshell.NavItem{
			{Href: "/activity", Label: "Activity", Key: "activity", Icon: "pulse", Hotkey: "l"},
			{Href: "/settings", Label: "Settings", Key: "settings", Icon: "gear", Hotkey: "s"},
		}},
	})
	c.Footer = template.HTML(template.HTMLEscapeString(s.deps.Name)) + " · " + template.HTML(template.HTMLEscapeString(c.T("a plan that deletes anything is applied in a terminal")))
	return c
}

func (s *Server) config() (config.Config, error) {
	return config.Load(s.deps.ConfigPath, nil)
}

func (s *Server) engine(names ...string) (*engine.Engine, error) {
	cfg, err := s.config()
	if err != nil {
		return nil, err
	}
	return engine.New(cfg, s.deps.Providers, names)
}

func (s *Server) record(kind, target, detail string, err error) {
	if s.deps.Activity == nil {
		return
	}
	if err != nil {
		detail = err.Error()
	}
	_ = s.deps.Activity.Append(activity.Entry{At: s.deps.Now(), Kind: kind, Target: target, Detail: detail, OK: err == nil})
}

func (s *Server) lastRun(name string) (run, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[name]
	return r, ok
}

func (s *Server) setRun(name string, r run) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[name] = r
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	toolshell.Render(w, s.tmpl, http.StatusOK, name, data)
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, code int, heading string, err error) {
	c := s.chrome(r, "")
	detail := c.T("Nothing lives at this address.")
	if err != nil {
		detail = err.Error()
	}
	toolshell.Render(w, s.tmpl, code, "ts-error", toolshell.ErrorPage{Chrome: c, Code: code, Heading: c.T(heading), Detail: detail})
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, http.StatusNotFound, "Page not found", nil)
}

func opChip(op string) string {
	switch plan.Op(op) {
	case plan.OpCreate:
		return "on"
	case plan.OpUpdate:
		return "warn"
	case plan.OpDelete:
		return "bad"
	}
	return "violet"
}

func localUser() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}

// ctx bounds a page's provider calls below the server's write timeout.
func ctx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 2*time.Minute)
}
