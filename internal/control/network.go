package control

import (
	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
	"net/http"
)

func (s *Server) GetPublicAddresses(w http.ResponseWriter, r *http.Request, id string) {
	v, e := s.node.ReadPublicAddresses(r.Context(), id)
	s.resultError(w, r, 200, v, e)
}
func (s *Server) ChangePublicAddresses(w http.ResponseWriter, r *http.Request, id string, _ httpapi.ChangePublicAddressesParams) {
	var request cluster.PublicAddressesPatch
	if !s.body(w, r, &request) {
		return
	}
	var out cluster.PublicAddresses
	e := s.node.Call(r.Context(), id, "public_addresses.patch", request, &out)
	s.resultError(w, r, 200, out, e)
}
func (s *Server) RefreshPublicAddresses(w http.ResponseWriter, r *http.Request, id string, _ httpapi.RefreshPublicAddressesParams) {
	var out cluster.PublicAddresses
	e := s.node.Call(r.Context(), id, "public_addresses.refresh", nil, &out)
	s.resultError(w, r, 200, out, e)
}
func (s *Server) GetPeerTrust(w http.ResponseWriter, r *http.Request) {
	v, e := s.node.TrustPolicy(r.Context())
	s.resultError(w, r, 200, v, e)
}
func (s *Server) ChangePeerTrust(w http.ResponseWriter, r *http.Request, _ httpapi.ChangePeerTrustParams) {
	var request cluster.TrustPolicyPatch
	if !s.body(w, r, &request) {
		return
	}
	v, e := s.node.ChangeTrust(r.Context(), request)
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
