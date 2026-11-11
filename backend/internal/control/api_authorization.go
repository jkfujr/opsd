package control

import (
	"context"
	"encoding/json"
	"github.com/jkfujr/opsd/internal/apiauth"
	"github.com/jkfujr/opsd/internal/cluster"
	"net/http"
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
	if session := current(r); session.session.Id != "" {
		return "account:" + session.owner + ":1"
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
	case "task_save", "task_run":
		return "tasks"
	case "compose", "compose_register":
		return "compose"
	}
	return ""
}
func (s *Server) authorizeAPI(r *http.Request, t apiauth.Token) (*http.Request, error) {
	policy := apiRequestOf(r.Context()).route
	if policy.Mode != "either" && policy.Mode != "bearer" {
		return nil, &cluster.Fault{Code: "forbidden", Message: "此接口需要管理员会话"}
	}
	module, id, level := policy.Module, parameterID(r, policy.Resource), policy.Level
	if directory, e := s.node.Directory(r.Context()); e == nil {
		type granted struct{ ID, Region string }
		members := []granted{}
		for _, member := range directory.Members {
			if !s.node.Active(r.Context(), member.ID) {
				continue
			}
			for _, grant := range t.Grants {
				if t.Allows(grant.Module, "read", member.ID, member.Region) {
					members = append(members, granted{member.ID, member.Region})
					break
				}
			}
		}
		body, _ := json.Marshal(members)
		apiRequestOf(r.Context()).scope = string(body)
	}
	switch policy.Scope {
	case "identity", "receipt":
		return r.WithContext(context.WithValue(r.Context(), apiAccessKey{}, &apiAccess{Token: t})), nil
	case "metrics":
		if !t.Has("monitoring", "read") && !t.Has("containers", "read") && !t.Has("databases", "read") {
			return nil, &cluster.Fault{Code: "forbidden", Message: "令牌没有指标读取权限"}
		}
		return r.WithContext(context.WithValue(r.Context(), apiAccessKey{}, &apiAccess{Token: t})), nil
	case "operation":
		op, e := s.node.GetOperation(r.Context(), id)
		if e != nil {
			return nil, e
		}
		module, id = operationModule(op), op.NodeID
		if module == "databases" {
			id = op.ResourceID
		}
	case "operations":
		allowed := false
		for _, m := range []string{"nodes", "containers", "firewall", "databases", "tasks", "compose"} {
			allowed = allowed || t.Has(m, "read")
		}
		if !allowed {
			return nil, &cluster.Fault{Code: "forbidden", Message: "令牌没有操作读取权限"}
		}
		return r.WithContext(context.WithValue(r.Context(), apiAccessKey{}, &apiAccess{Token: t})), nil
	}
	if module == "" || !t.Has(module, level) {
		return nil, &cluster.Fault{Code: "forbidden", Message: "令牌没有此接口权限"}
	}
	if id == "" && module != "databases" {
		id = r.URL.Query().Get("node_id")
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
	return r.WithContext(context.WithValue(r.Context(), apiAccessKey{}, &apiAccess{Token: t, Module: module})), nil
}
