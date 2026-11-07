package control

import (
	"net/http"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/httpapi"
)

func (s *Server) GetNavigation(w http.ResponseWriter, r *http.Request) {
	value, e := s.node.Navigation(r.Context())
	s.resultError(w, r, 200, convertHTTP[httpapi.NavigationPreferences](value), e)
}

func (s *Server) UpdateNavigation(w http.ResponseWriter, r *http.Request, _ httpapi.UpdateNavigationParams) {
	var input httpapi.NavigationPreferences
	if !s.body(w, r, &input) {
		return
	}
	if input.Revision < 0 {
		s.fail(w, r, 422, "invalid_request", "菜单版本无效")
		return
	}
	value, e := s.node.SaveNavigation(r.Context(), convertHTTP[cluster.NavigationPreferences](input))
	s.resultError(w, r, 200, convertHTTP[httpapi.NavigationPreferences](value), e)
}
