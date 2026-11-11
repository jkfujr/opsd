package control

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func conditionResource(s *Server, r *http.Request) string {
	state := apiRequestOf(r.Context())
	if state == nil {
		return ""
	}
	owner := r.PathValue("node_id")
	if owner == "" {
		owner = s.node.ID
	}
	id := r.PathValue("id")
	if id == "" {
		id = r.PathValue("task_id")
	}
	if id == "" {
		id = r.PathValue("installation_id")
	}
	if id == "" {
		id = r.PathValue("instance_id")
	}
	if state.route.Version == "compose-registration" {
		id = r.PathValue("engine_id") + "/" + r.PathValue("project_id")
	}
	return state.route.Version + "/" + owner + "/" + id
}
func entityTag(resource string, revision any) string {
	return `"` + base64.RawURLEncoding.EncodeToString([]byte(resource+"/"+fmt.Sprint(revision))) + `"`
}
func (s *Server) setETag(w http.ResponseWriter, r *http.Request, revision any) {
	w.Header().Set("ETag", entityTag(conditionResource(s, r), revision))
}
func (s *Server) prepareCondition(w http.ResponseWriter, r *http.Request) bool {
	state := apiRequestOf(r.Context())
	if state.route.Condition == "" {
		return true
	}
	match, none := r.Header.Get("If-Match"), r.Header.Get("If-None-Match")
	if state.route.Condition == "create-or-match" && none == "*" && match == "" {
		value := uint64(0)
		state.expected = &value
		state.create = true
		return true
	}
	if match == "" {
		s.fail(w, r, 428, "precondition_required", "请读取资源并提供 If-Match")
		return false
	}
	if none != "" || strings.HasPrefix(match, "W/") || !strings.HasPrefix(match, `"`) || !strings.HasSuffix(match, `"`) {
		s.fail(w, r, 400, "malformed_request", "并发条件格式无效")
		return false
	}
	raw, e := base64.RawURLEncoding.DecodeString(strings.Trim(match, `"`))
	prefix := conditionResource(s, r) + "/"
	if e != nil || !strings.HasPrefix(string(raw), prefix) {
		s.fail(w, r, 412, "precondition_failed", "并发条件不属于当前资源")
		return false
	}
	state.expectedValue = strings.TrimPrefix(string(raw), prefix)
	if state.route.Version == "peer-trust" || state.route.Version == "public-addresses" || state.route.Version == "compose-registration" {
		return true
	}
	value, e := strconv.ParseUint(state.expectedValue, 10, 64)
	if e != nil {
		s.fail(w, r, 400, "malformed_request", "资源版本无效")
		return false
	}
	state.expected = &value
	return true
}
func expectedRevision(r *http.Request) uint64 {
	if state := apiRequestOf(r.Context()); state != nil && state.expected != nil {
		return *state.expected
	}
	return 0
}
func (s *Server) checkRevision(w http.ResponseWriter, r *http.Request, revision uint64) bool {
	if state := apiRequestOf(r.Context()); state != nil && state.expected != nil && revision != *state.expected {
		s.fail(w, r, 412, "precondition_failed", "资源已经改变，请重新读取")
		return false
	}
	return true
}
func requestFingerprint(r *http.Request) string {
	state := apiRequestOf(r.Context())
	if state == nil {
		return ""
	}
	var data any
	if len(state.body) > 0 {
		if json.Unmarshal(state.body, &data) != nil {
			data = string(state.body)
		}
	}
	canonical, _ := json.Marshal(struct {
		Operation, Path, Condition, Create string
		Body                               any
	}{state.route.OperationID, r.URL.Path, r.Header.Get("If-Match"), r.Header.Get("If-None-Match"), data})
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}
func (s *Server) mergeBody(w http.ResponseWriter, r *http.Request, current any, out any) bool {
	encoded, e := json.Marshal(current)
	if e != nil {
		s.fail(w, r, 500, "internal_error", "无法读取保存值")
		return false
	}
	var target map[string]json.RawMessage
	if json.Unmarshal(encoded, &target) != nil {
		s.fail(w, r, 500, "internal_error", "保存值格式无效")
		return false
	}
	var patch map[string]json.RawMessage
	if !s.body(w, r, &patch) {
		return false
	}
	mergeJSONObjects(target, patch)
	encoded, _ = json.Marshal(target)
	if json.Unmarshal(encoded, out) != nil {
		s.fail(w, r, 422, "validation_failed", "修改后的字段格式无效")
		return false
	}
	return true
}
func mergeJSONObjects(target, patch map[string]json.RawMessage) {
	for key, value := range patch {
		if string(value) == "null" {
			delete(target, key)
			continue
		}
		var left, right map[string]json.RawMessage
		if json.Unmarshal(value, &right) == nil && right != nil {
			_ = json.Unmarshal(target[key], &left)
			if left == nil {
				left = map[string]json.RawMessage{}
			}
			mergeJSONObjects(left, right)
			target[key], _ = json.Marshal(left)
		} else {
			target[key] = value
		}
	}
}
