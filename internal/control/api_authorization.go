package control

import (
	"context"
	"net/http"
	"strings"

	"github.com/jkfujr/opsd/internal/apiauth"
	"github.com/jkfujr/opsd/internal/cluster"
)

type apiAccess struct {
	Token  apiauth.Token
	Module string
}
type apiAccessKey struct{}

func access(ctx context.Context) *apiAccess { a, _ := ctx.Value(apiAccessKey{}).(*apiAccess); return a }
func actor(r *http.Request) string {
	if a := access(r.Context()); a != nil {
		return a.Token.Actor()
	}
	return ""
}
func operationModule(op cluster.Operation) string {
	switch op.Subject {
	case "container":
		return "containers"
	case "name":
		return "nodes"
	case "firewall", "firewall_config", "component":
		return "firewall"
	case "database":
		return "databases"
	}
	return ""
}
func (s *Server) authorizeAPI(r *http.Request, t apiauth.Token) (*http.Request, error) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	p := strings.Split(path, "/")
	module, id := "", ""
	level := "read"
	if r.Method != "GET" && r.Method != "HEAD" {
		level = "manage"
	}
	switch {
	case path == "api-tokens/verify":
		return r.WithContext(context.WithValue(r.Context(), apiAccessKey{}, &apiAccess{Token: t})), nil
	case r.URL.Path == "/metrics":
		if !t.Has("monitoring", "read") && !t.Has("containers", "read") && !t.Has("databases", "read") {
			return nil, &cluster.Fault{Code: "forbidden", Message: "令牌没有指标读取权限"}
		}
		return r.WithContext(context.WithValue(r.Context(), apiAccessKey{}, &apiAccess{Token: t})), nil
	case path == "monitoring/current" || path == "monitoring/history":
		module = "monitoring"
		id = r.URL.Query().Get("node_id")
	case path == "containers":
		module = "containers"
		id = r.URL.Query().Get("node_id")
	case path == "nodes":
		module = "nodes"
	case len(p) >= 2 && p[0] == "nodes":
		id = p[1]
		if len(p) == 2 && (r.Method == "GET" || r.Method == "PATCH") {
			module = "nodes"
		} else if len(p) >= 3 && p[2] == "containers" {
			module = "containers"
		} else if len(p) >= 3 && p[2] == "firewall" {
			module = "firewall"
		}
	case len(p) >= 2 && p[0] == "operations":
		if p[1] == "pending" {
			break
		}
		op, e := s.node.GetOperation(r.Context(), p[1])
		if e != nil {
			return nil, e
		}
		module, id = operationModule(op), op.NodeID
		if module == "databases" {
			id = op.ResourceID
		}
	case path == "operations":
	case len(p) >= 2 && p[0] == "databases" && p[1] == "clusters":
		module = "databases"
		if len(p) >= 3 {
			id = p[2]
		}
		if len(p) == 2 && level == "manage" || len(p) == 3 && (r.Method == "PATCH" || r.Method == "DELETE") {
			module = ""
		}
	}
	if path == "operations" || path == "operations/pending" {
		if !t.Has("nodes", "read") && !t.Has("containers", "read") && !t.Has("firewall", "read") && !t.Has("databases", "read") {
			return nil, &cluster.Fault{Code: "forbidden", Message: "令牌没有操作读取权限"}
		}
	} else {
		if module == "" || !t.Has(module, level) {
			return nil, &cluster.Fault{Code: "forbidden", Message: "令牌没有此操作权限"}
		}
		if id != "" {
			region := ""
			if module != "databases" {
				m, e := s.node.Member(r.Context(), id)
				if e != nil {
					return nil, e
				}
				region = m.Region
			}
			if !t.Allows(module, level, id, region) {
				return nil, &cluster.Fault{Code: "forbidden", Message: "资源超出令牌授权范围"}
			}
		}
	}
	return r.WithContext(context.WithValue(r.Context(), apiAccessKey{}, &apiAccess{Token: t, Module: module})), nil
}
