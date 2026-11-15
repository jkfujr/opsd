package cluster

import (
	"encoding/json"
	"net/http"
)

// LocalHandler 由仅本机账号和 root 可访问的 Unix Socket 提供。
func (n *Node) LocalHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /shared", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(n.SharedStatus(r.Context())) })
	mux.HandleFunc("POST /shared/initialize", func(w http.ResponseWriter, r *http.Request) {
		var input SharedInitialize
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&input) != nil {
			http.Error(w, "请求无效", 422)
			return
		}
		op, err := n.StartSharedInitialization(r.Context(), input, "shared-init/"+input.ClusterID+"/"+input.Database)
		if err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		json.NewEncoder(w).Encode(op)
	})
	mux.HandleFunc("POST /shared/recover", func(w http.ResponseWriter, r *http.Request) {
		if err := n.RecoverSharedState(r.Context()); err != nil {
			http.Error(w, err.Error(), 409)
			return
		}
		json.NewEncoder(w).Encode(n.SharedStatus(r.Context()))
	})
	mux.HandleFunc("GET /self", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(map[string]string{"id": n.ID}) })
	mux.HandleFunc("GET /operations/{id}", func(w http.ResponseWriter, r *http.Request) {
		op, e := n.operation(r.Context(), r.PathValue("id"))
		if e != nil {
			http.Error(w, "操作不存在", 404)
			return
		}
		json.NewEncoder(w).Encode(op)
	})
	mux.HandleFunc("POST /command", func(w http.ResponseWriter, r *http.Request) {
		var request Request
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&request) != nil {
			http.Error(w, "请求无效", 422)
			return
		}
		if request.Kind == "console" || request.Kind == "api" {
			var value struct {
				Enabled bool `json:"enabled"`
			}
			if json.Unmarshal(request.Payload, &value) != nil {
				http.Error(w, "请求无效", 422)
				return
			}
			c, e := n.Config(r.Context())
			if e != nil {
				http.Error(w, "配置不可用", 503)
				return
			}
			if request.Kind == "console" {
				c.Console = value.Enabled
				if value.Enabled {
					c.CoordinatorCandidate = true
				}
			} else {
				c.API = value.Enabled
			}
			request.Kind = "config"
			request.Payload, _ = json.Marshal(c)
		} else if request.Kind != "account" {
			http.Error(w, "未知本机操作", 422)
			return
		}
		op, e := n.enqueue(r.Context(), n.ID, request)
		if e != nil {
			http.Error(w, "请求未受理", 409)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(op)
	})
	return mux
}
