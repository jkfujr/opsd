package control

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"

	"github.com/jkfujr/opsd/internal/httpapi"
)

type apiContextKey struct{}
type apiRequest struct {
	route         httpapi.APIRoute
	request       *http.Request
	body          []byte
	expected      *uint64
	expectedValue string
	scope         string
	dispatched    bool
	create        bool
}

func apiRequestOf(ctx context.Context) *apiRequest {
	value, _ := ctx.Value(apiContextKey{}).(*apiRequest)
	return value
}
func strictRequest(ctx context.Context) *http.Request {
	value := apiRequestOf(ctx)
	r := value.request.Clone(ctx)
	if value.body != nil {
		r.Body = io.NopCloser(bytes.NewReader(value.body))
	}
	return r
}

type apiResponse struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func newAPIResponse(r *http.Request) *apiResponse {
	w := &apiResponse{header: make(http.Header)}
	w.header.Set("X-Request-ID", r.Header.Get("X-Request-ID"))
	return w
}
func (w *apiResponse) Header() http.Header { return w.header }
func (w *apiResponse) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
}
func (w *apiResponse) Write(data []byte) (int, error) {
	if w.code == 0 {
		w.code = 200
	}
	return w.body.Write(data)
}
func (w *apiResponse) status() int {
	if w.code == 0 {
		return 200
	}
	return w.code
}

// 生成响应的正文和响应头始终经过对应的强类型结构。
func strictResponse[T any](w *apiResponse) (T, error) {
	var value T
	var fill func(reflect.Value) error
	fill = func(v reflect.Value) error {
		if v.Kind() == reflect.Struct {
			if body := v.FieldByName("Body"); body.IsValid() {
				if w.body.Len() > 0 {
					if err := json.Unmarshal(w.body.Bytes(), body.Addr().Interface()); err != nil {
						return err
					}
				}
				if headers := v.FieldByName("Headers"); headers.IsValid() {
					fillResponseHeaders(headers, w.header)
				}
				return nil
			}
			if headers := v.FieldByName("Headers"); headers.IsValid() {
				fillResponseHeaders(headers, w.header)
				return nil
			}
			if v.NumField() == 1 && v.Type().Field(0).Anonymous {
				return fill(v.Field(0))
			}
		}
		if w.body.Len() > 0 {
			return json.Unmarshal(w.body.Bytes(), v.Addr().Interface())
		}
		return nil
	}
	err := fill(reflect.ValueOf(&value).Elem())
	return value, err
}
func fillResponseHeaders(v reflect.Value, h http.Header) {
	names := map[string]string{"XRequestID": "X-Request-ID", "WWWAuthenticate": "WWW-Authenticate", "RetryAfter": "Retry-After", "ETag": "ETag", "Etag": "ETag", "SetCookie": "Set-Cookie"}
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		name := v.Type().Field(i).Name
		if mapped, ok := names[name]; ok {
			name = mapped
		}
		value := h.Get(name)
		if field.Kind() == reflect.String {
			field.SetString(value)
		} else if field.Kind() == reflect.Pointer && field.Type().Elem().Kind() == reflect.String && value != "" {
			p := reflect.New(field.Type().Elem())
			p.Elem().SetString(value)
			field.Set(p)
		}
	}
}

func (s *Server) streamAPI(w http.ResponseWriter, r *http.Request) {
	switch apiRequestOf(r.Context()).route.OperationID {
	case "getEvents":
		s.events(w, r)
	case "getMetrics":
		s.metrics(w, r)
	case "getOpenAPISpec":
		s.GetOpenAPISpec(w, r)
	case "getAPIDocumentation":
		s.GetAPIDocumentation(w, r)
	case "attachTerminal":
		s.AttachTerminal(w, r, r.PathValue("ticket"))
	case "installTheme":
		s.InstallTheme(w, r, httpapi.InstallThemeParams{})
	default:
		s.fail(w, r, 500, "internal_error", "流式接口未登记")
	}
}

func (s *Server) GetEvents(w http.ResponseWriter, r *http.Request)  { s.events(w, r) }
func (s *Server) GetMetrics(w http.ResponseWriter, r *http.Request) { s.metrics(w, r) }
func (s *Server) GetOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(httpapi.OpenAPI)
}
func (s *Server) GetAPIDocumentation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>OPSD API v2</title><link rel="stylesheet" href="/api/v2/docs-assets/swagger-ui.css"></head><body><form id="authentication" style="padding:16px;font:14px system-ui"><label>Bearer 令牌 <input id="token" type="password" autocomplete="off"></label> <button type="submit">打开接口文档</button><span id="error" role="status"></span></form><div id="swagger-ui"></div><script src="/api/v2/docs-assets/swagger-ui-bundle.js"></script><script>
let bearer='',csrf='';
async function openDocs(){const headers=bearer?{Authorization:'Bearer '+bearer}:{};const response=await fetch('/api/v2/openapi.json',{headers,credentials:bearer?'omit':'same-origin'});if(!response.ok){let p=await response.json();document.getElementById('error').textContent=p.detail||'请登录或填写令牌';return;}if(!bearer){let s=await fetch('/api/v2/auth/sessions/current');if(s.ok)csrf=(await s.json()).csrf_token;}
SwaggerUIBundle({spec:await response.json(),dom_id:'#swagger-ui',persistAuthorization:false,validatorUrl:null,tryItOutEnabled:false,queryConfigEnabled:false,requestInterceptor:request=>{if(new URL(request.url,location.href).origin!==location.origin)throw Error('仅支持当前节点');if(bearer){request.headers.Authorization='Bearer '+bearer;request.credentials='omit';}else{request.credentials='same-origin';if(!['GET','HEAD'].includes(request.method))request.headers['X-CSRF-Token']=csrf;}return request;}});document.getElementById('error').textContent='';}
document.getElementById('authentication').onsubmit=e=>{e.preventDefault();bearer=document.getElementById('token').value.trim();openDocs().catch(()=>document.getElementById('error').textContent='读取失败，请重试');};openDocs().catch(()=>{});
</script></body></html>`)
}

func (s *Server) GetAPIIdentity(w http.ResponseWriter, r *http.Request) {
	principal := httpapi.APIPrincipal{Kind: httpapi.APIPrincipalKindAccount, Id: actor(r)}
	grants := []httpapi.APIGrant{}
	if a := access(r.Context()); a != nil {
		principal.Kind = httpapi.APIPrincipalKindToken
		principal.Id = a.Token.Actor()
		grants = convertHTTP[[]httpapi.APIGrant](a.Token.Grants)
	}
	write(w, 200, httpapi.APIIdentity{ApiVersion: httpapi.APIVersion, NodeVersion: s.options.NodeVersion, FrontendVersion: s.options.FrontendVersion, NodeId: s.node.ID, Principal: principal, Grants: grants, OpenapiUrl: httpapi.APIBasePath + "/openapi.json", DocsUrl: httpapi.APIBasePath + "/docs"})
}

func apiPattern(route httpapi.APIRoute) string {
	if route.Path == "/metrics" {
		return route.Method + " /metrics"
	}
	path := httpapi.APIBasePath + route.Path
	if route.Path == "/" {
		path += "{$}"
	}
	return route.Method + " " + path
}
func isAPIPath(path string) bool {
	return path == "/api" || strings.HasPrefix(path, "/api/") || path == httpapi.APIBasePath || path == "/metrics"
}
func parameterID(r *http.Request, name string) string {
	if name == "" {
		return ""
	}
	return r.PathValue(name)
}
