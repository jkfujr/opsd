package control

import (
	"encoding/json"
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"net/http"
)

func (s *Server) GetPublicAddresses(w http.ResponseWriter, r *http.Request, id string) {
	v, e := s.node.ReadPublicAddresses(r.Context(), id)
	if e == nil && !v.Cached {
		s.setETag(w, r, cluster.PublicConfigurationVersion(v))
	}
	s.resultError(w, r, 200, v, e)
}
func (s *Server) ChangePublicAddresses(w http.ResponseWriter, r *http.Request, id string, _ httpapi.ChangePublicAddressesParams) {
	current, e := s.node.ReadPublicAddresses(r.Context(), id)
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	if current.Cached {
		s.fail(w, r, 503, "dependency_unavailable", "节点暂不可达")
		return
	}
	var input struct {
		IPv4 cluster.PublicFamilyPatch `json:"ipv4"`
		IPv6 cluster.PublicFamilyPatch `json:"ipv6"`
	}
	base := map[string]any{"ipv4": cluster.PublicFamilyPatch{Mode: current.IPv4.Mode, Addresses: current.IPv4.Addresses}, "ipv6": cluster.PublicFamilyPatch{Mode: current.IPv6.Mode, Addresses: current.IPv6.Addresses}}
	if !s.mergeBody(w, r, base, &input) {
		return
	}
	request := cluster.PublicAddressesPatch{IPv4: &input.IPv4, IPv6: &input.IPv6}
	request.ExpectedConfiguration = apiRequestOf(r.Context()).expectedValue
	var out cluster.PublicAddresses
	e = s.node.Call(r.Context(), id, "public_addresses.patch", request, &out)
	if e == nil {
		s.setETag(w, r, cluster.PublicConfigurationVersion(out))
	}
	s.resultError(w, r, 200, out, e)
}
func (s *Server) RefreshPublicAddresses(w http.ResponseWriter, r *http.Request, id string, _ httpapi.RefreshPublicAddressesParams) {
	var input httpapi.RefreshPublicAddressesJSONRequestBody
	if !s.body(w, r, &input) {
		return
	}
	var out cluster.PublicAddresses
	e := s.node.Call(r.Context(), id, "public_addresses.refresh", map[string]uint64{"expected_revision": uint64(input.ExpectedRevision)}, &out)
	s.resultError(w, r, 200, out, e)
}
func (s *Server) GetPeerTrust(w http.ResponseWriter, r *http.Request) {
	v, e := s.node.TrustPolicy(r.Context())
	if e == nil {
		b, _ := json.Marshal(v.Version)
		s.setETag(w, r, string(b))
	}
	s.resultError(w, r, 200, v, e)
}
func (s *Server) ChangePeerTrust(w http.ResponseWriter, r *http.Request, _ httpapi.ChangePeerTrustParams) {
	var input httpapi.PeerTrustPatch
	if !s.body(w, r, &input) {
		return
	}
	current, e := s.node.TrustPolicy(r.Context())
	if e != nil {
		s.operationError(w, r, e)
		return
	}
	var expected cluster.TrustVersion
	if json.Unmarshal([]byte(apiRequestOf(r.Context()).expectedValue), &expected) != nil {
		s.fail(w, r, 412, "precondition_failed", "互信设置已经改变，请重新读取")
		return
	}
	if expected != current.Version {
		s.fail(w, r, 412, "precondition_failed", "互信设置已经改变，请重新读取")
		return
	}
	if input.Enabled == nil {
		s.setETag(w, r, apiRequestOf(r.Context()).expectedValue)
		write(w, 200, current)
		return
	}
	request := cluster.TrustPolicyPatch{Enabled: *input.Enabled, ExpectedVersion: expected}
	v, e := s.node.ChangeTrust(r.Context(), request)
	if e == nil {
		b, _ := json.Marshal(v.Version)
		s.setETag(w, r, string(b))
	}
	s.resultError(w, r, 200, v, e)
}
func (s *Server) GetPeerTrustStatus(w http.ResponseWriter, r *http.Request) {
	v, e := s.node.TrustStatus(r.Context())
	s.resultError(w, r, 200, v, e)
}
func (s *Server) GetNodePeerTrust(w http.ResponseWriter, r *http.Request, id string) {
	var out cluster.TrustRules
	e := s.node.Call(r.Context(), id, "peer_trust.rules", nil, &out)
	s.resultError(w, r, 200, out, e)
}
