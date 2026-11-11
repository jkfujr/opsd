package control

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/identity"
)

var testRequestConditions sync.Map

func validateTestResponse(t *testing.T, request *http.Request, response *http.Response) {
	t.Helper()
	var route *httpapi.APIRoute
	for i := range httpapi.APIRoutes {
		candidate := &httpapi.APIRoutes[i]
		if request.Method == candidate.Method && matchTemplate(httpapi.APIBasePath+candidate.Path, request.URL.Path) {
			route = candidate
			break
		}
	}
	if route == nil || route.Stream {
		return
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	response.Body = io.NopCloser(bytes.NewReader(body))
	if response.StatusCode >= 400 {
		t.Logf("%s %s：%d %s", request.Method, request.URL.Path, response.StatusCode, body)
	}
	doc, err := apiContract()
	if err != nil {
		t.Fatal(err)
	}
	item := doc.Paths.Value(route.Path)
	input := &openapi3filter.ResponseValidationInput{RequestValidationInput: &openapi3filter.RequestValidationInput{Request: request, Route: &routers.Route{Spec: doc, Path: route.Path, PathItem: item, Method: route.Method, Operation: item.GetOperation(route.Method)}}, Status: response.StatusCode, Header: response.Header, Options: &openapi3filter.Options{IncludeResponseStatus: true}}
	input.SetBodyBytes(body)
	if err = openapi3filter.ValidateResponse(t.Context(), input); err != nil {
		t.Fatalf("%s %s 应答不符合 OpenAPI：%v；%s", request.Method, request.URL.Path, err, body)
	}
}

// 业务用例共享 v2 客户端的媒体类型、请求键与条件头处理；契约用例直接发送原始请求。
func prepareTestRequest(t *testing.T, client *http.Client, request *http.Request) {
	t.Helper()
	var route *httpapi.APIRoute
	for i := range httpapi.APIRoutes {
		candidate := &httpapi.APIRoutes[i]
		if request.Method == candidate.Method && matchTemplate(httpapi.APIBasePath+candidate.Path, request.URL.Path) {
			route = candidate
			break
		}
	}
	if route == nil {
		return
	}
	if request.Method == http.MethodPatch {
		request.Header.Set("Content-Type", "application/merge-patch+json")
	}
	if route.Idempotent && request.Header.Get("Idempotency-Key") == "" {
		request.Header.Set("Idempotency-Key", identity.Random())
	}
	var data map[string]json.RawMessage
	var original []byte
	if request.Body != nil && request.Header.Get("Content-Type") != "application/zip" {
		original, _ = io.ReadAll(request.Body)
		_ = json.Unmarshal(original, &data)
	}
	if route.Condition != "" && request.Header.Get("If-Match") == "" && request.Header.Get("If-None-Match") == "" {
		var revision *int64
		if value := data["revision"]; len(value) > 0 {
			_ = json.Unmarshal(value, &revision)
		}
		if route.Condition == "create-or-match" && revision != nil && *revision == 0 {
			request.Header.Set("If-None-Match", "*")
		} else {
			cache := t.Name() + "/" + request.URL.String() + "/" + request.Header.Get("Authorization") + "/" + request.Header.Get("Idempotency-Key")
			tag := ""
			if request.Header.Get("Idempotency-Key") != "" {
				if saved, ok := testRequestConditions.Load(cache); ok {
					tag = saved.(string)
				}
			}
			if tag == "" {
				read, _ := http.NewRequestWithContext(request.Context(), "GET", request.URL.String(), nil)
				read.Header.Set("Authorization", request.Header.Get("Authorization"))
				// 空 Authorization 不能覆盖管理员会话。
				if read.Header.Get("Authorization") == "" {
					read.Header.Del("Authorization")
				}
				response, err := client.Do(read)
				if err != nil {
					t.Fatal(err)
				}
				if response.StatusCode == 404 && route.Condition == "create-or-match" {
					request.Header.Set("If-None-Match", "*")
				}
				tag = response.Header.Get("ETag")
				response.Body.Close()
				if tag != "" && revision != nil && *revision >= 0 {
					raw, _ := base64.RawURLEncoding.DecodeString(strings.Trim(tag, `"`))
					if index := strings.LastIndexByte(string(raw), '/'); index >= 0 {
						tag = `"` + base64.RawURLEncoding.EncodeToString([]byte(string(raw[:index+1])+strconv.FormatInt(*revision, 10))) + `"`
					}
				}
				if tag != "" && request.Header.Get("Idempotency-Key") != "" {
					testRequestConditions.Store(cache, tag)
				}
			}
			if request.Header.Get("If-None-Match") == "" {
				request.Header.Set("If-Match", tag)
			}
		}
		if revision == nil || *revision >= 0 {
			delete(data, "revision")
		}
	}
	if route.OperationID == "createAPIToken" {
		delete(data, "revision")
	}
	if route.OperationID == "changeNodeSettings" {
		delete(data, "cached")
		delete(data, "last_synced_at")
		var storage map[string]json.RawMessage
		if json.Unmarshal(data["storage"], &storage) == nil && storage != nil {
			delete(storage, "native_cluster")
			data["storage"], _ = json.Marshal(storage)
		}
	}
	if route.OperationID == "saveScheduledTask" {
		delete(data, "id")
		delete(data, "node_id")
		delete(data, "node_name")
		delete(data, "next_run_at")
		delete(data, "last_operation_id")
		delete(data, "last_state")
		delete(data, "updated_at")
	}
	if len(original) > 0 {
		if data != nil {
			original, _ = json.Marshal(data)
		}
		if bytes.Equal(original, []byte("null")) {
			original = nil
		}
		if request.Method == http.MethodDelete {
			original = nil
		}
		request.Body = io.NopCloser(bytes.NewReader(original))
		request.ContentLength = int64(len(original))
	}
}
