package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/consoletheme"
	"github.com/jkfujr/opsd/internal/httpapi"
)

func (s *Server) cookieName() string { return "opsd_session_" + s.node.ID }

type Options struct {
	EnforceListenerState bool
	Assets               fs.FS
	Node                 *cluster.Node
	NodeVersion          string
	FrontendVersion      string
	ThemeDir             string
	Now                  func() time.Time
}
type subscriber struct {
	session string
	events  chan string
	done    chan struct{}
}
type Server struct {
	db              *sql.DB
	node            *cluster.Node
	options         Options
	now             func() time.Time
	mu              sync.Mutex
	subscribers     map[*subscriber]struct{}
	failures        map[string][]time.Time
	inFlight        map[string]int
	passwordSlots   chan struct{}
	terminalTickets map[string]terminalTicket
	themes          *consoletheme.Manager
}
type sessionContext struct {
	session  httpapi.Session
	csrf     string
	entrance string
	owner    string
}
type contextKey struct{}

func New(ctx context.Context, db *sql.DB, options Options) (*Server, error) {
	if options.Node == nil {
		return nil, errors.New("缺少本机节点")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	s := &Server{db: db, node: options.Node, options: options, now: options.Now, subscribers: map[*subscriber]struct{}{}, failures: map[string][]time.Time{}, inFlight: map[string]int{}, passwordSlots: make(chan struct{}, 1)}
	if options.ThemeDir != "" {
		version := options.NodeVersion
		if version == "" || version == "dev" {
			version = options.FrontendVersion
		}
		var err error
		s.themes, err = consoletheme.Open(ctx, db, options.ThemeDir, version)
		if err != nil {
			var fileError *os.PathError
			if s.themes == nil || !errors.As(err, &fileError) {
				return nil, err
			}
			slog.Warn("主题目录未准备完成，内置控制台继续运行", "error", err)
		}
	}
	return s, nil
}

func sameOrigin(r *http.Request) bool {
	origin, err := url.Parse(r.Header.Get("Origin"))
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return err == nil && origin.Scheme == scheme && strings.EqualFold(origin.Host, r.Host) && origin.User == nil && origin.Path == "" && origin.RawQuery == "" && origin.Fragment == ""
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) authenticate(r *http.Request) (sessionContext, error) {
	cookie, err := r.Cookie(s.cookieName())
	if err != nil || len(cookie.Value) > 128 {
		return sessionContext{}, sql.ErrNoRows
	}
	var value sessionContext
	value.owner = s.node.ID
	var created, expires, last int64
	err = s.db.QueryRowContext(r.Context(), `SELECT s.id,a.username,s.csrf_token,s.created_at,s.expires_at,s.last_seen_at,s.remote_address,a.entrance FROM sessions s CROSS JOIN account a WHERE s.token_hash=? AND s.revoked_at IS NULL AND s.expires_at>? AND a.id=1`, digest(cookie.Value), s.now().Unix()).Scan(&value.session.Id, &value.session.Username, &value.csrf, &created, &expires, &last, &value.session.RemoteAddress, &value.entrance)
	if err != nil {
		return sessionContext{}, err
	}
	value.session.CreatedAt = time.Unix(created, 0).UTC()
	value.session.ExpiresAt = time.Unix(expires, 0).UTC()
	value.session.LastSeenAt = time.Unix(last, 0).UTC()
	value.session.Current = true
	if s.now().Unix()-last >= 60 {
		_, err = s.db.ExecContext(r.Context(), "UPDATE sessions SET last_seen_at=? WHERE id=? AND last_seen_at<?", s.now().Unix(), value.session.Id, s.now().Unix()-59)
	}
	return value, err
}

func current(r *http.Request) sessionContext {
	value, _ := r.Context().Value(contextKey{}).(sessionContext)
	return value
}

func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func (s *Server) body(w http.ResponseWriter, r *http.Request, value any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/merge-patch+json") {
		s.fail(w, r, 415, "unsupported_media_type", "请求须使用 JSON")
		return false
	}
	limit := int64(256 * 1024)
	reader := http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		s.fail(w, r, 422, "invalid_request", "请求内容无效")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		s.fail(w, r, 422, "invalid_request", "请求内容无效")
		return false
	}
	return true
}

func (s *Server) result(w http.ResponseWriter, r *http.Request, status int, value any, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		s.fail(w, r, 404, "not_found", "记录不存在")
		return
	}
	if err != nil {
		s.fail(w, r, 500, "internal_error", "操作失败，请重试")
		return
	}
	write(w, status, value)
}

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	path := r.URL.Path
	if strings.HasPrefix(path, consoletheme.AssetsPath) {
		s.themeAsset(w, r)
		return
	}
	if strings.HasPrefix(path, "/assets/") && s.options.Assets != nil {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.FileServerFS(s.options.Assets).ServeHTTP(w, r)
		return
	}
	if path == "/api" || strings.HasPrefix(path, "/api/") || path == "/metrics" {
		http.NotFound(w, r)
		return
	}
	var entrance string
	if err := s.db.QueryRowContext(r.Context(), "SELECT entrance FROM account WHERE id=1").Scan(&entrance); err != nil {
		http.NotFound(w, r)
		return
	}
	builtin := path == strings.TrimSuffix(consoletheme.BuiltinPath, "/") || strings.HasPrefix(path, consoletheme.BuiltinPath)
	if path != entrance {
		if _, err := s.authenticate(r); err != nil {
			http.NotFound(w, r)
			return
		}
	}
	if builtin {
		if path == strings.TrimSuffix(consoletheme.BuiltinPath, "/") {
			http.Redirect(w, r, consoletheme.BuiltinPath, http.StatusTemporaryRedirect)
			return
		}
		s.builtinPage(w, r)
		return
	}
	if strings.HasPrefix(path, "/__opsd/") {
		http.NotFound(w, r)
		return
	}
	forceBuiltin := path == entrance && r.URL.Query().Get("builtin") == "1"
	hadTheme := false
	if !forceBuiltin && s.themes != nil {
		item, data, err := s.themes.ActiveIndex(r.Context())
		if err != nil {
			s.themeError(w, r, err)
			return
		}
		hadTheme = item.InstallationID != ""
		if data != nil && (path != entrance || item.Manifest.Login == "theme") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(data)
			return
		}
	}
	if path != entrance && !hadTheme {
		if path != "/" && path != "/containers" && path != "/databases" && !strings.HasPrefix(path, "/databases/") && path != "/storage" && path != "/firewall" && path != "/firewall/settings" && path != "/nodes" && !strings.HasPrefix(path, "/nodes/") && path != "/settings" && !strings.HasPrefix(path, "/settings/") && path != "/tools" && path != "/tools/tasks" && path != "/tools/terminal" && path != "/terminal" {
			http.NotFound(w, r)
			return
		}
	}
	s.builtinPage(w, r)
}

func (s *Server) builtinPage(w http.ResponseWriter, r *http.Request) {
	if s.options.Assets == nil {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(s.options.Assets, "index.html")
	if err != nil {
		s.fail(w, r, 500, "internal_error", "界面资源不可用")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func (s *Server) Close() {
	s.revokeStreams("")
	if s.themes != nil {
		s.themes.Close()
	}
}

func (s *Server) notify(event string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sub := range s.subscribers {
		select {
		case sub.events <- event:
		default:
			select {
			case <-sub.events:
			default:
			}
			select {
			case sub.events <- "all":
			default:
			}
		}
	}
}

func (s *Server) revokeStreams(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sub := range s.subscribers {
		if id == "" || sub.session == id {
			close(sub.done)
			delete(s.subscribers, sub)
		}
	}
}
