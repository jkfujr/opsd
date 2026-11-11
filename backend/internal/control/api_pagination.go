package control

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
)

type collectionCursor struct {
	Query    string            `json:"query"`
	After    map[string]string `json:"after,omitempty"`
	Position string            `json:"position,omitempty"`
}

func cursorQuery(r *http.Request) string {
	q := r.URL.Query()
	q.Del("cursor")
	scope := actor(r)
	if state := apiRequestOf(r.Context()); state != nil {
		scope += "/" + state.scope
	}
	if a := access(r.Context()); a != nil {
		scope += "/" + strconv.FormatUint(a.Token.Revision, 10)
		b, _ := json.Marshal(a.Token.Grants)
		scope += "/" + string(b)
	}
	sum := sha256.Sum256([]byte(r.URL.Path + "?" + q.Encode() + "/" + scope))
	return hex.EncodeToString(sum[:])
}
func encodeCursor(r *http.Request, c collectionCursor) string {
	c.Query = cursorQuery(r)
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}
func decodeCursor(r *http.Request) (collectionCursor, error) {
	var c collectionCursor
	raw := r.URL.Query().Get("cursor")
	if raw == "" {
		return c, nil
	}
	if len(raw) > 32000 {
		return c, fmt.Errorf("游标超过上限")
	}
	b, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil || json.Unmarshal(b, &c) != nil || c.Query != cursorQuery(r) {
		return c, fmt.Errorf("分页条件或授权已经改变，请重新查询")
	}
	return c, nil
}
func collectionID(item map[string]any) string {
	value := func(k string) string { s, _ := item[k].(string); return s }
	id := value("id")
	if id == "" {
		id = value("installation_id")
	}
	if id == "" {
		id = value("container_id")
	}
	if id == "" {
		if cluster, ok := item["cluster"].(map[string]any); ok {
			id, _ = cluster["id"].(string)
		}
	}
	return value("node_id") + "/" + value("engine_id") + "/" + id
}
func collectionOwner(item map[string]any) string {
	if node, ok := item["node_id"].(string); ok {
		return node
	}
	if cluster, ok := item["cluster"].(map[string]any); ok {
		owner, _ := cluster["coordinator"].(string)
		return owner
	}
	return ""
}
func (s *Server) prepareAPIResponse(w *apiResponse, r *http.Request) {
	state := apiRequestOf(r.Context())
	if state == nil || w.status() < 200 || w.status() >= 300 || !state.route.Pagination || state.route.OperationID == "listOperations" {
		return
	}
	var value map[string]any
	if json.Unmarshal(w.body.Bytes(), &value) != nil {
		return
	}
	items, ok := value["items"].([]any)
	if !ok {
		return
	}
	partial := false
	if nodes, ok := value["nodes"].([]any); ok {
		for _, raw := range nodes {
			node, _ := raw.(map[string]any)
			read, _ := node["read"].(map[string]any)
			state, _ := read["state"].(string)
			if state == "offline" || state == "unavailable" {
				partial = true
			}
		}
	}
	if len(items) == 0 {
		if nodes, ok := value["nodes"].([]any); ok && len(nodes) > 0 {
			unavailable := true
			for _, raw := range nodes {
				node, _ := raw.(map[string]any)
				read, _ := node["read"].(map[string]any)
				state, _ := read["state"].(string)
				if state != "offline" && state != "unavailable" {
					unavailable = false
				}
			}
			if unavailable {
				w.body.Reset()
				w.code = 0
				s.fail(w, r, 503, "dependency_unavailable", "请求涉及的节点暂不可用")
				return
			}
		}
	}
	cursor, e := decodeCursor(r)
	if e != nil {
		w.body.Reset()
		w.code = 0
		s.fail(w, r, 400, "malformed_request", e.Error())
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, _ := items[i].(map[string]any)
		right, _ := items[j].(map[string]any)
		return collectionID(left) < collectionID(right)
	})
	filtered := []any{}
	for _, item := range items {
		v, _ := item.(map[string]any)
		if after := cursor.After[collectionOwner(v)]; after == "" || collectionID(v) > after {
			filtered = append(filtered, item)
		}
	}
	delete(value, "next_cursor")
	if len(filtered) > limit || partial {
		if len(filtered) > limit {
			filtered = filtered[:limit]
		}
		positions := cursor.After
		if positions == nil {
			positions = map[string]string{}
		}
		for _, raw := range filtered {
			item, _ := raw.(map[string]any)
			positions[collectionOwner(item)] = collectionID(item)
		}
		value["next_cursor"] = encodeCursor(r, collectionCursor{After: positions})
	}
	value["items"] = filtered
	w.body.Reset()
	_ = json.NewEncoder(&w.body).Encode(value)
}
