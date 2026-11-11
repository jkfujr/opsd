package control

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/identity"
)

var contractOnce sync.Once
var contractDoc *openapi3.T
var contractError error

type apiTraceKey struct{}
type apiTrace struct{ Route, Actor, Target string }

func apiContract() (*openapi3.T, error) {
	contractOnce.Do(func() {
		contractDoc, contractError = openapi3.NewLoader().LoadFromData(httpapi.OpenAPI)
		if contractError == nil {
			contractError = contractDoc.Validate(context.Background())
		}
	})
	return contractDoc, contractError
}

func (s *Server) Handler() http.Handler {
	strict := httpapi.NewStrictHandlerWithOptions(&strictAPI{server: s}, nil, httpapi.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			s.fail(w, r, 400, "malformed_request", "请求内容无法解析")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("API 响应不符合契约", "operation", apiRequestOf(r.Context()).route.OperationID, "request_id", r.Header.Get("X-Request-ID"), "error", err)
			s.fail(w, r, 500, "internal_error", "接口应答未完成，请查询原请求")
		},
	})
	inner := httpapi.HandlerWithOptions(strict, httpapi.StdHTTPServerOptions{BaseURL: httpapi.APIBasePath, ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
		s.fail(w, r, 400, "malformed_request", "请求参数无法解析")
	}})
	mux := http.NewServeMux()
	for _, route := range httpapi.APIRoutes {
		mux.HandleFunc(apiPattern(route), func(w http.ResponseWriter, r *http.Request) { s.serveAPI(w, r, route, inner) })
	}
	mux.HandleFunc("GET "+httpapi.APIBasePath, func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = httpapi.APIBasePath + "/"
		mux.ServeHTTP(w, r)
	})
	mux.HandleFunc("GET "+httpapi.APIBasePath+"/docs-assets/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name != "swagger-ui.css" && name != "swagger-ui-bundle.js" {
			s.fail(w, r, 404, "not_found", "文档资源不存在")
			return
		}
		data, err := httpapi.DocumentationAssets.ReadFile("static/" + name)
		if err != nil {
			s.fail(w, r, 503, "dependency_unavailable", "文档资源不可用")
			return
		}
		if strings.HasSuffix(name, ".css") {
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		} else {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		}
		w.Write(data)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) {
			allowed := []string{}
			for _, route := range httpapi.APIRoutes {
				path := httpapi.APIBasePath + route.Path
				if route.Path == "/metrics" {
					path = "/metrics"
				}
				if matchTemplate(path, r.URL.Path) {
					allowed = append(allowed, route.Method)
					if route.Method == "GET" {
						allowed = append(allowed, "HEAD")
					}
				}
			}
			if len(allowed) > 0 {
				slices.Sort(allowed)
				w.Header().Set("Allow", strings.Join(slices.Compact(allowed), ", "))
				s.fail(w, r, 405, "method_not_allowed", "此资源不支持该请求方法")
			} else {
				s.fail(w, r, 404, "not_found", "接口不存在")
			}
			return
		}
		config, err := s.node.Config(r.Context())
		if err != nil {
			s.operationError(w, r, err)
			return
		}
		if s.options.EnforceListenerState && !config.Console {
			http.NotFound(w, r)
			return
		}
		s.page(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		id := identity.Random()
		r.Header.Set("X-Request-ID", id)
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		out := &apiWriter{ResponseWriter: w, head: r.Method == http.MethodHead}
		trace := &apiTrace{Route: "unknown"}
		r = r.WithContext(context.WithValue(r.Context(), apiTraceKey{}, trace))
		mux.ServeHTTP(out, r)
		if isAPIPath(r.URL.Path) {
			slog.Info("HTTP 请求", "method", r.Method, "route", trace.Route, "actor", trace.Actor, "target", trace.Target, "status", out.status(), "request_id", id, "duration_ms", time.Since(started).Milliseconds())
		}
	})
}

func (s *Server) serveAPI(w http.ResponseWriter, r *http.Request, route httpapi.APIRoute, inner http.Handler) {
	trace, _ := r.Context().Value(apiTraceKey{}).(*apiTrace)
	if trace != nil {
		trace.Route = route.Path
		trace.Target = r.PathValue("node_id")
	}
	state := &apiRequest{route: route}
	r = r.WithContext(context.WithValue(r.Context(), apiContextKey{}, state))
	state.request = r
	config, err := s.node.Config(r.Context())
	if err != nil {
		s.operationError(w, r, err)
		return
	}
	_, hasAuthorization := r.Header["Authorization"]
	if hasAuthorization || route.Mode == "bearer" {
		if !config.API {
			s.fail(w, r, 503, "dependency_unavailable", "API 已关闭")
			return
		}
		parts := strings.Fields(r.Header.Get("Authorization"))
		if !hasAuthorization {
			s.fail(w, r, 401, "unauthenticated", "请提供 Bearer 令牌")
			return
		}
		if len(r.Header.Values("Authorization")) != 1 || len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			s.fail(w, r, 400, "malformed_request", "Authorization 格式无效")
			return
		}
		verifyCtx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		token, e := s.node.VerifyAPIToken(verifyCtx, parts[1], remoteIP(r))
		cancel()
		if e != nil {
			s.operationError(w, r, e)
			return
		}
		authorized, e := s.authorizeAPI(r, token)
		if e != nil {
			s.operationError(w, r, e)
			return
		}
		r = authorized
	} else if route.Mode != "anonymous" {
		if s.options.EnforceListenerState && !config.Console {
			s.fail(w, r, 404, "not_found", "网页已关闭")
			return
		}
		session, e := s.authenticate(r)
		if e != nil {
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
	} else if s.options.EnforceListenerState && (!config.API && !config.Console || route.OperationID == "login" && !config.Console) {
		s.fail(w, r, 404, "not_found", "Web 服务已关闭")
		return
	}
	state.request = r
	if trace != nil {
		trace.Actor = actor(r)
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
		target := r.PathValue("node_id")
		if route.OperationID == "revokeNode" {
			target = ""
		}
		if strings.HasPrefix(route.Path, "/operations/") {
			target = cluster.OperationOwner(r.PathValue("id"))
		}
		if target != "" {
			if e := s.node.RequireAPIAuthorization(r.Context(), target); e != nil {
				s.operationError(w, r, e)
				return
			}
		}
	}
	if route.Idempotent && !validRequestKey(r.Header.Get("Idempotency-Key")) {
		s.fail(w, r, 400, "malformed_request", "缺少有效 Idempotency-Key")
		return
	}
	if !s.prepareCondition(w, r) {
		return
	}
	if route.OperationID == "getMetrics" {
		s.streamAPI(w, r)
		return
	}
	doc, e := apiContract()
	if e != nil {
		slog.Error("加载接口规范失败", "error", e)
		s.fail(w, r, 500, "internal_error", "接口规范不可用")
		return
	}
	item := doc.Paths.Value(route.Path)
	operation := item.GetOperation(route.Method)
	accept := r.Header.Get("Accept")
	if accept != "" && !strings.Contains(accept, "*/*") && !strings.Contains(accept, "application/json") && !strings.Contains(accept, "application/problem+json") && !route.Stream {
		s.fail(w, r, 406, "not_acceptable", "接口仅提供 JSON 应答")
		return
	}
	media, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if operation.RequestBody != nil {
		if mediaErr != nil || operation.RequestBody.Value.Content[media] == nil {
			s.fail(w, r, 415, "unsupported_media_type", "请求 Content-Type 不受支持")
			return
		}
	}
	jsonBody := !route.Stream || route.OperationID == "installTheme" && media == "application/json"
	if jsonBody && r.Body != nil {
		limit := route.MaxBody
		if route.OperationID == "installTheme" {
			limit = 262144
		}
		data, e := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
		if e != nil {
			var large *http.MaxBytesError
			if errors.As(e, &large) {
				s.fail(w, r, 413, "content_too_large", "请求内容超过上限")
			} else {
				s.fail(w, r, 400, "malformed_request", "请求内容未接收完整")
			}
			return
		}
		state.body = data
		r.Body = io.NopCloser(bytes.NewReader(data))
		if len(data) > 0 && (media == "application/json" || media == "application/merge-patch+json") && !json.Valid(data) {
			s.fail(w, r, 400, "malformed_request", "JSON 格式无效")
			return
		}
		if operation.RequestBody == nil && len(data) > 0 {
			s.fail(w, r, 400, "malformed_request", "此接口不接受请求正文")
			return
		}
	}
	params := map[string]string{}
	for _, part := range strings.Split(route.Path, "/") {
		if strings.HasPrefix(part, "{") {
			name := strings.Trim(part, "{}")
			params[name] = r.PathValue(name)
		}
	}
	input := &openapi3filter.RequestValidationInput{Request: r, PathParams: params, Route: &routers.Route{Spec: doc, Path: route.Path, PathItem: item, Method: route.Method, Operation: operation}, Options: &openapi3filter.Options{ExcludeRequestBody: !jsonBody, AuthenticationFunc: openapi3filter.NoopAuthenticationFunc, SkipSettingDefaults: true}}
	if e := openapi3filter.ValidateRequest(r.Context(), input); e != nil {
		s.validationProblem(w, r, e)
		return
	}
	if state.body != nil {
		r.Body = io.NopCloser(bytes.NewReader(state.body))
	}
	state.request = r
	state.dispatched = true
	if route.Stream {
		s.streamAPI(w, r)
	} else {
		inner.ServeHTTP(w, r)
	}
}

func matchTemplate(pattern, path string) bool {
	a, b := strings.Split(strings.TrimSuffix(pattern, "/"), "/"), strings.Split(strings.TrimSuffix(path, "/"), "/")
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.HasPrefix(a[i], "{") && strings.HasSuffix(a[i], "}") {
			if b[i] == "" {
				return false
			}
			continue
		}
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func validRequestKey(key string) bool {
	if len(key) < 1 || len(key) > 128 {
		return false
	}
	for _, c := range key {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._:-", c) {
			continue
		}
		return false
	}
	return true
}

func (s *Server) validationProblem(w http.ResponseWriter, r *http.Request, err error) {
	status, code := 422, "validation_failed"
	location, pointer := "body", "/"
	var requestError *openapi3filter.RequestError
	var parseError *openapi3filter.ParseError
	var schemaError *openapi3.SchemaError
	if errors.As(err, &requestError) && requestError.Parameter != nil {
		location = requestError.Parameter.In
		pointer = requestError.Parameter.Name
	}
	if errors.As(err, &parseError) {
		status, code = 400, "malformed_request"
	}
	if errors.As(err, &schemaError) {
		if parts := schemaError.JSONPointer(); len(parts) > 0 {
			pointer = "/" + strings.Join(parts, "/")
		}
	}
	p := newProblem(r, status, code, "请求字段不符合接口要求")
	p.Errors = &[]httpapi.ProblemField{{Location: httpapi.ProblemFieldLocation(location), Pointer: pointer, Code: code, Message: "请按接口定义检查此字段"}}
	writeProblem(w, p)
}
func newProblem(r *http.Request, status int, code, message string) httpapi.Problem {
	id := r.Header.Get("X-Request-ID")
	if id == "" {
		id = identity.Random()
	}
	return httpapi.Problem{Type: "urn:opsd:problem:" + code, Title: message, Status: status, Detail: message, Instance: "urn:opsd:request:" + id, Code: code, RequestId: id}
}
func writeProblem(w http.ResponseWriter, p httpapi.Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("X-Request-ID", p.RequestId)
	w.WriteHeader(p.Status)
	json.NewEncoder(w).Encode(p)
}
func (s *Server) fail(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	if code == "invalid_request" && status == 422 {
		code = "validation_failed"
	}
	p := newProblem(r, status, code, message)
	if status == 401 && (r.Header.Get("Authorization") != "" || r.URL.Path == "/metrics") {
		w.Header().Set("WWW-Authenticate", `Bearer realm="opsd", error="invalid_token"`)
	}
	if status == 403 && r.Header.Get("Authorization") != "" {
		w.Header().Set("WWW-Authenticate", `Bearer realm="opsd", error="insufficient_scope"`)
	}
	if state := apiRequestOf(r.Context()); state != nil && state.route.Idempotent {
		outcome := httpapi.ProblemOutcomeNotAccepted
		if status >= 500 && state.dispatched {
			outcome = httpapi.ProblemOutcomeUnknown
		}
		p.Outcome = &outcome
	}
	writeProblem(w, p)
}

type apiWriter struct {
	http.ResponseWriter
	code int
	head bool
}

func (w *apiWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *apiWriter) status() int {
	if w.code == 0 {
		return 200
	}
	return w.code
}
func (w *apiWriter) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
		w.ResponseWriter.WriteHeader(code)
	}
}
func (w *apiWriter) Write(data []byte) (int, error) {
	if w.code == 0 {
		w.WriteHeader(200)
	}
	if w.head {
		return len(data), nil
	}
	return w.ResponseWriter.Write(data)
}
func (w *apiWriter) Flush() { http.NewResponseController(w.ResponseWriter).Flush() }
func (w *apiWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}
