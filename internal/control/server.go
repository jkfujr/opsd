package control

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/opsd-labs/opsd/internal/httpapi"
	"github.com/opsd-labs/opsd/internal/identity"
	"github.com/opsd-labs/opsd/internal/transport"
)

const cookieName = "opsd_session"

type Options struct {
	Assets          fs.FS
	ControlAddress  string
	ControlVersion  string
	FrontendVersion string
	Now             func() time.Time
}

type connection struct {
	channel  *transport.Channel
	id       string
	writable bool
}

type subscriber struct {
	session string
	events  chan string
	done    chan struct{}
}

type Server struct {
	db            *sql.DB
	key           identity.Key
	options       Options
	now           func() time.Time
	mu            sync.Mutex
	lifecycle     sync.Mutex
	connections   map[string]*connection
	subscribers   map[*subscriber]struct{}
	failures      map[string][]time.Time
	inFlight      map[string]int
	passwordSlots chan struct{}
}

type sessionContext struct {
	session  httpapi.Session
	csrf     string
	entrance string
}
type contextKey struct{}

func New(ctx context.Context, db *sql.DB, options Options) (*Server, error) {
	key, err := LoadKey(ctx, db)
	if err != nil {
		return nil, err
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	var entrance string
	if err := db.QueryRowContext(ctx, "SELECT entrance FROM account WHERE id=1").Scan(&entrance); err != nil {
		return nil, err
	}
	return &Server{db: db, key: key, options: options, now: options.Now, connections: make(map[string]*connection), subscribers: make(map[*subscriber]struct{}), failures: make(map[string][]time.Time), inFlight: make(map[string]int), passwordSlots: make(chan struct{}, 2)}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	httpapi.HandlerWithOptions(s, httpapi.StdHTTPServerOptions{BaseURL: "/api/v1", BaseRouter: mux, ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
		s.fail(w, r, 422, "invalid_request", "请求参数无效")
	}})
	mux.HandleFunc("GET /api/v1/agent/connect", s.connect)
	mux.HandleFunc("GET /api/v1/events", s.events)
	mux.HandleFunc("/", s.page)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path == "/api/v1/agent/connect" {
			mux.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/v1/auth/login" {
			session, err := s.authenticate(r)
			if err != nil {
				s.fail(w, r, 401, "unauthenticated", "请重新登录")
				return
			}
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				if !sameOrigin(r) || r.Header.Get("X-CSRF-Token") != session.csrf {
					s.fail(w, r, 403, "csrf_rejected", "请求校验失败，请刷新页面")
					return
				}
			}
			r = r.WithContext(context.WithValue(r.Context(), contextKey{}, session))
		}
		mux.ServeHTTP(w, r)
	})
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
	cookie, err := r.Cookie(cookieName)
	if err != nil || len(cookie.Value) > 128 {
		return sessionContext{}, sql.ErrNoRows
	}
	var value sessionContext
	var created, expires, last int64
	err = s.db.QueryRowContext(r.Context(), `SELECT s.id,a.username,s.csrf_token,s.created_at,s.expires_at,s.last_seen_at,s.remote_address,a.entrance FROM sessions s CROSS JOIN account a WHERE s.token_hash=? AND s.revoked_at IS NULL AND s.expires_at>? AND a.id=1`, digest(cookie.Value), s.now().Unix()).Scan(&value.session.Id, &value.session.Username, &value.csrf, &created, &expires, &last, &value.session.RemoteAddress, &value.entrance)
	if err != nil {
		return sessionContext{}, err
	}
	value.session.CreatedAt = time.Unix(created, 0).UTC()
	value.session.ExpiresAt = time.Unix(expires, 0).UTC()
	value.session.LastSeenAt = time.Unix(last, 0).UTC()
	value.session.Current = true
	if s.now().Unix() > last {
		_, err = s.db.ExecContext(r.Context(), "UPDATE sessions SET last_seen_at=? WHERE id=?", s.now().Unix(), value.session.Id)
	}
	return value, err
}

func current(r *http.Request) sessionContext { return r.Context().Value(contextKey{}).(sessionContext) }

func (s *Server) fail(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	write(w, status, httpapi.APIError{Code: httpapi.APIErrorCode(code), Message: message, RequestId: identity.Random()})
}

func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func (s *Server) body(w http.ResponseWriter, r *http.Request, value any) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		s.fail(w, r, 422, "invalid_request", "请求须使用 JSON")
		return false
	}
	reader := http.MaxBytesReader(w, r.Body, 16*1024)
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
	if strings.HasPrefix(path, "/assets/") && s.options.Assets != nil {
		http.FileServerFS(s.options.Assets).ServeHTTP(w, r)
		return
	}
	var entrance string
	if err := s.db.QueryRowContext(r.Context(), "SELECT entrance FROM account WHERE id=1").Scan(&entrance); err != nil {
		http.NotFound(w, r)
		return
	}
	if path != entrance {
		if path != "/" && path != "/settings" && !strings.HasPrefix(path, "/settings/") {
			http.NotFound(w, r)
			return
		}
		if _, err := s.authenticate(r); err != nil {
			http.NotFound(w, r)
			return
		}
	}
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
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, conn := range s.connections {
		conn.channel.Close()
	}
	for sub := range s.subscribers {
		close(sub.done)
		delete(s.subscribers, sub)
	}
}

func (s *Server) notify(event string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sub := range s.subscribers {
		select {
		case sub.events <- event:
		default:
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
