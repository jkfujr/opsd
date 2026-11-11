package control

import (
	"database/sql"
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkfujr/opsd/internal/cluster"
	"github.com/jkfujr/opsd/internal/consoletheme"
	"github.com/jkfujr/opsd/internal/httpapi"
	"github.com/jkfujr/opsd/internal/requeststate"
)

func (s *Server) themeError(w http.ResponseWriter, r *http.Request, err error) {
	var fault *consoletheme.Error
	var tooLarge *http.MaxBytesError
	var fileError *os.PathError
	switch {
	case errors.As(err, &tooLarge):
		s.fail(w, r, 413, "invalid_request", "主题 ZIP 不能超过 100 MiB")
	case errors.As(err, &fault):
		s.operationError(w, r, &cluster.Fault{Code: fault.Code, Message: fault.Message})
	case errors.Is(err, sql.ErrNoRows):
		s.fail(w, r, 404, "not_found", "主题版本不存在")
	case errors.As(err, &fileError):
		s.fail(w, r, 503, "dependency_unavailable", "主题目录不可用，请检查节点文件权限后重新读取")
	default:
		s.fail(w, r, 500, "internal_error", "主题操作未完成，请重新读取列表")
	}
}

func (s *Server) themeReady(w http.ResponseWriter, r *http.Request) bool {
	if s.themes != nil {
		return true
	}
	s.fail(w, r, 503, "dependency_unavailable", "主题目录未配置")
	return false
}

func (s *Server) ListThemes(w http.ResponseWriter, r *http.Request, _ httpapi.ListThemesParams) {
	list := consoletheme.List{Items: []consoletheme.Installation{}, BuiltinURL: consoletheme.BuiltinPath}
	if s.themes != nil {
		var err error
		list, err = s.themes.List(r.Context())
		if err != nil {
			s.themeError(w, r, err)
			return
		}
	}
	write(w, 200, convertHTTP[httpapi.ThemeList](list))
}

func (s *Server) InstallTheme(w http.ResponseWriter, r *http.Request, _ httpapi.InstallThemeParams) {
	if !s.themeReady(w, r) {
		return
	}
	r = r.WithContext(requeststate.With(r.Context(), writeIdentity(r)))
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if media == "application/zip" {
		s.uploadTheme(w, r)
		return
	}
	var input httpapi.ThemeRepositoryInstall
	if !s.body(w, r, &input) {
		return
	}
	tag := ""
	if input.Tag != nil {
		tag = *input.Tag
	}
	item, err := s.themes.InstallGitHub(r.Context(), input.Repository, tag)
	if err != nil {
		s.themeError(w, r, err)
		return
	}
	s.notify("themes")
	w.Header().Set("Location", httpapi.APIBasePath+"/console/theme-installations/"+item.InstallationID)
	write(w, 201, convertHTTP[httpapi.ThemeInstallation](item))
}

func (s *Server) uploadTheme(w http.ResponseWriter, r *http.Request) {
	if !s.themeReady(w, r) {
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/zip" {
		s.fail(w, r, 422, "invalid_request", "请上传 ZIP 主题包")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, consoletheme.MaxArchiveSize)
	item, err := s.themes.Install(r.Context(), r.Body, "upload")
	if err != nil {
		s.themeError(w, r, err)
		return
	}
	s.notify("themes")
	w.Header().Set("Location", httpapi.APIBasePath+"/console/theme-installations/"+item.InstallationID)
	write(w, 201, convertHTTP[httpapi.ThemeInstallation](item))
}

func (s *Server) ActivateTheme(w http.ResponseWriter, r *http.Request, _ httpapi.ActivateThemeParams) {
	if !s.themeReady(w, r) {
		return
	}
	var input httpapi.ThemeSelection
	if !s.body(w, r, &input) {
		return
	}
	if input.InstallationId == "" {
		s.fail(w, r, 422, "invalid_request", "请选择主题版本或 builtin")
		return
	}
	id := input.InstallationId
	if id == "builtin" {
		id = ""
	}
	if err := s.themes.ActivateRevision(r.Context(), id, apiRequestOf(r.Context()).expected); err != nil {
		s.themeError(w, r, err)
		return
	}
	s.notify("themes")
	s.GetThemeSelection(w, r)
}

func (s *Server) DeleteTheme(w http.ResponseWriter, r *http.Request, id string, _ httpapi.DeleteThemeParams) {
	if !s.themeReady(w, r) {
		return
	}
	if err := s.themes.DeleteRevision(r.Context(), id, apiRequestOf(r.Context()).expected); err != nil {
		s.themeError(w, r, err)
		return
	}
	s.notify("themes")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) themeAsset(w http.ResponseWriter, r *http.Request) {
	if s.themes == nil {
		http.NotFound(w, r)
		return
	}
	id, name, ok := strings.Cut(strings.TrimPrefix(r.URL.Path, consoletheme.AssetsPath), "/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, err := s.themes.OpenAsset(r.Context(), id, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if strings.ToLower(filepath.Ext(name)) != ".html" {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeContent(w, r, name, info.ModTime(), f)
}
