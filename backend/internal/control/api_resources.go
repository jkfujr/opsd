package control

import (
	"database/sql"
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/protocol"
	"github.com/jkfujr/opsd/internal/requeststate"
	"net/http"
)

func (s *Server) GetNodeStatus(w http.ResponseWriter, r *http.Request, id string) {
	value, e := s.findNode(r.Context(), id)
	if e == nil && value.Connected {
		var capabilities struct {
			AuthorizationVersion int `json:"authorization_version"`
		}
		if err := s.node.Call(r.Context(), id, "api.capabilities", nil, &capabilities); err == nil {
			value.ApiAuthorizationVersion = &capabilities.AuthorizationVersion
		}
	}
	s.result(w, r, 200, value, e)
}
func (s *Server) GetFirewallStatus(w http.ResponseWriter, r *http.Request, id string) {
	var value protocol.ManagedFirewallState
	e := s.node.Call(r.Context(), id, "firewall.config.read", nil, &value)
	s.resultError(w, r, 200, convertHTTP[httpapi.ManagedFirewallState](value), e)
}
func (s *Server) FirewallAction(w http.ResponseWriter, r *http.Request, id string, _ httpapi.FirewallActionParams) {
	var input httpapi.FirewallAction
	if !s.body(w, r, &input) {
		return
	}
	switch input.Action {
	case "install":
		if input.Backend == nil {
			s.fail(w, r, 422, "validation_failed", "请选择防火墙后端")
			return
		}
		revision := uint64(input.ExpectedRevision)
		s.submit(w, r, id, "component", protocol.ComponentAction{Component: "firewall", Action: "install", Backend: protocol.Backend(*input.Backend), ExpectedRevision: &revision}, r.Header.Get("Idempotency-Key"), "")
	case "sync":
		if input.ExpectedRevision < 1 {
			s.fail(w, r, 422, "validation_failed", "缺少配置版本")
			return
		}
		s.writeFirewallConfig(w, r, id, cluster.FirewallWrite{ExpectedRevision: uint64(input.ExpectedRevision), Key: r.Header.Get("Idempotency-Key")})
	case "adopt_current":
		if input.Fingerprint == nil || len(*input.Fingerprint) != 64 || input.ExpectedRevision < 1 {
			s.fail(w, r, 422, "invalid_request", "请先读取当前防火墙配置")
			return
		}
		s.writeFirewallConfig(w, r, id, cluster.FirewallWrite{ExpectedRevision: uint64(input.ExpectedRevision), AdoptFingerprint: *input.Fingerprint, Key: r.Header.Get("Idempotency-Key")})
	}
}

func (s *Server) PreviewFirewallAdoption(w http.ResponseWriter, r *http.Request, id string) {
	var preview cluster.FirewallAdoption
	err := s.node.Call(r.Context(), id, "firewall.config.preview_adoption", nil, &preview)
	s.resultError(w, r, 200, convertHTTP[httpapi.FirewallAdoption](preview), err)
}
func (s *Server) DecideOperation(w http.ResponseWriter, r *http.Request, id string, _ httpapi.DecideOperationParams) {
	var input httpapi.OperationAction
	if !s.body(w, r, &input) {
		return
	}
	var op cluster.Operation
	var e error
	if input.Action == "cancel" {
		op, e = s.node.CallOperationDecision(r.Context(), id, "operation.cancel", cluster.OperationDecision{ID: id, ExpectedRevision: uint64(input.ExpectedRevision)}, writeIdentity(r))
	} else {
		decision := cluster.OperationDecision{ID: id, ExpectedRevision: uint64(input.ExpectedRevision)}
		if input.AcceptCurrent != nil {
			decision.AcceptCurrent = *input.AcceptCurrent
		}
		op, e = s.node.CallOperationDecision(r.Context(), id, "operation."+string(input.Action), decision, writeIdentity(r))
	}
	s.resultError(w, r, 200, convertHTTP[httpapi.Operation](op), e)
}
func (s *Server) GetDatabaseDefinition(w http.ResponseWriter, r *http.Request, id string) {
	c, e := s.node.DatabaseCluster(r.Context(), id)
	if e == nil {
		e = s.node.Call(r.Context(), c.Coordinator, "database.cluster.get", cluster.DatabaseRPC{ResourceID: id}, &c)
	}
	if e == nil {
		s.setETag(w, r, c.Revision)
	}
	s.resultError(w, r, 200, convertHTTP[httpapi.ManagedDatabaseCluster](c), e)
}
func (s *Server) GetDatabaseInstance(w http.ResponseWriter, r *http.Request, node, id string) {
	var item cluster.DatabaseInstance
	e := s.node.Call(r.Context(), node, "database.instance.get", cluster.DatabaseRPC{InstanceID: id}, &item)
	if e == nil {
		s.setETag(w, r, item.Revision)
	}
	s.resultError(w, r, 200, convertHTTP[httpapi.DatabaseInstance](item), e)
}
func (s *Server) GetScheduledTask(w http.ResponseWriter, r *http.Request, node, id string) {
	var item cluster.ScheduledTask
	e := s.node.Call(r.Context(), node, "task.get", id, &item)
	s.resultError(w, r, 200, convertHTTP[httpapi.ScheduledTaskMetadata](item), e)
}
func (s *Server) GetScheduledTaskDefinition(w http.ResponseWriter, r *http.Request, node, id string) {
	var item cluster.ScheduledTask
	e := s.node.Call(r.Context(), node, "task.get", id, &item)
	if e == nil {
		s.setETag(w, r, item.Revision)
	}
	s.resultError(w, r, 200, convertHTTP[httpapi.ScheduledTaskDefinition](item), e)
}
func (s *Server) GetComposeProject(w http.ResponseWriter, r *http.Request, node, engine, id string) {
	var projects []protocol.ComposeProject
	e := s.node.Call(r.Context(), node, "compose.list", nil, &projects)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	for _, project := range projects {
		if project.ID == id && project.EngineID == engine {
			item := convertHTTP[httpapi.ComposeProjectItem](project)
			item.NodeId = node
			write(w, 200, item)
			return
		}
	}
	s.fail(w, r, 404, "not_found", "Compose 项目不存在")
}
func (s *Server) GetThemeInstallation(w http.ResponseWriter, r *http.Request, id string) {
	if !s.themeReady(w, r) {
		return
	}
	list, e := s.themes.List(r.Context())
	if e != nil {
		s.themeError(w, r, e)
		return
	}
	for _, item := range list.Items {
		if item.InstallationID == id {
			s.setETag(w, r, uint64(item.InstalledAt.Unix()))
			write(w, 200, convertHTTP[httpapi.ThemeInstallation](item))
			return
		}
	}
	s.fail(w, r, 404, "not_found", "主题版本不存在")
}
func (s *Server) GetThemeSelection(w http.ResponseWriter, r *http.Request) {
	var id sql.NullString
	var revision uint64
	if e := s.db.QueryRowContext(r.Context(), "SELECT installation_id,revision FROM console_theme_selection WHERE id=1").Scan(&id, &revision); e != nil {
		s.operationError(w, r, e)
		return
	}
	selection := "builtin"
	if id.Valid {
		selection = id.String
	}
	effective := "builtin"
	if s.themes != nil {
		list, e := s.themes.List(r.Context())
		if e != nil {
			s.themeError(w, r, e)
			return
		}
		if list.EffectiveInstallationID != "" {
			effective = list.EffectiveInstallationID
		}
	}
	s.setETag(w, r, revision)
	write(w, 200, httpapi.ThemeSelectionState{InstallationId: selection, EffectiveInstallationId: effective, Revision: int64(revision), BuiltinUrl: "/__opsd/builtin/"})
}
func writeIdentity(r *http.Request) *requeststate.Write {
	return &requeststate.Write{Actor: actor(r), Key: r.Header.Get("Idempotency-Key"), Digest: requestFingerprint(r)}
}
func (s *Server) GetRequestReceipt(w http.ResponseWriter, r *http.Request, key string, p httpapi.GetRequestReceiptParams) {
	value, e := s.node.RequestReceipt(r.Context(), p.NodeId, requeststate.Write{Actor: actor(r), Key: key})
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	if a := access(r.Context()); a != nil {
		module := ""
		node := p.NodeId
		if value.Operation != nil {
			module = operationModule(*value.Operation)
			if module == "databases" {
				node = value.Operation.ResourceID
			}
		}
		region := ""
		if module != "databases" {
			member, err := s.node.Member(r.Context(), p.NodeId)
			if err != nil {
				s.operationError(w, r, err)
				return
			}
			region = member.Region
		}
		if module == "" || !a.Token.Allows(module, "read", node, region) {
			s.fail(w, r, 403, "forbidden", "请求超出当前授权范围")
			return
		}
	}
	out := httpapi.RequestReceipt{Key: key, Owner: value.Owner, Actor: value.Actor, State: httpapi.RequestReceiptState(value.State), CreatedAt: value.CreatedAt}
	if value.OperationID != "" {
		out.OperationId = &value.OperationID
	}
	if url := receiptResourceURL(value); url != "" {
		out.ResourceUrl = &url
	}
	if value.Secret {
		available := false
		out.SecretAvailable = &available
		missing := true
		out.SecretUnavailable = &missing
	}
	write(w, 200, out)
}
func receiptResourceURL(value cluster.RequestReceipt) string {
	if value.OperationID != "" {
		return httpapi.APIBasePath + "/operations/" + value.OperationID
	}
	switch value.Kind {
	case "api-token":
		return httpapi.APIBasePath + "/nodes/" + value.Owner + "/api-tokens/" + value.ResourceID
	case "theme":
		return httpapi.APIBasePath + "/console/theme-installations/" + value.ResourceID
	case "enrollment":
		return httpapi.APIBasePath + "/enrollment-tokens/" + value.ResourceID
	case "database":
		return httpapi.APIBasePath + "/database-clusters/" + value.ResourceID
	}
	return ""
}
