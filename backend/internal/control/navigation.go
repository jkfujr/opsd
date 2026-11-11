package control

import (
	"net/http"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
)

func (s *Server) GetNavigation(w http.ResponseWriter, r *http.Request) {
	value, e := s.node.Navigation(r.Context())
	if e == nil {
		s.setETag(w, r, value.Revision)
	}
	s.resultError(w, r, 200, convertHTTP[httpapi.NavigationPreferences](value), e)
}

func (s *Server) UpdateNavigation(w http.ResponseWriter, r *http.Request, _ httpapi.UpdateNavigationParams) {
	var input httpapi.NavigationInput
	if !s.body(w, r, &input) {
		return
	}
	prefs := convertHTTP[cluster.NavigationPreferences](input)
	prefs.Revision = expectedRevision(r)
	value, e := s.node.SaveNavigation(r.Context(), prefs)
	if e == nil {
		s.setETag(w, r, value.Revision)
	}
	s.resultError(w, r, 200, convertHTTP[httpapi.NavigationPreferences](value), e)
}
