// Code generated from api/openapi.json and api.gen.go; DO NOT EDIT.
package control

import (
	"context"
	"fmt"
	"github.com/jkfujr/opsd/internal/httpapi"
)

type strictAPI struct{ server *Server }

var _ httpapi.StrictServerInterface = (*strictAPI)(nil)

func (a *strictAPI) GetAPIIdentity(ctx context.Context, request httpapi.GetAPIIdentityRequestObject) (httpapi.GetAPIIdentityResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetAPIIdentity(w, r)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetAPIIdentity200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetAPIIdentity400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetAPIIdentity401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetAPIIdentity403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetAPIIdentity404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetAPIIdentity405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetAPIIdentity406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetAPIIdentity409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetAPIIdentity412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetAPIIdentity413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetAPIIdentity415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetAPIIdentity422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetAPIIdentity428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetAPIIdentity429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetAPIIdentity500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetAPIIdentity503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetAPIIdentity504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetAPIIdentity 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ListSessions(ctx context.Context, request httpapi.ListSessionsRequestObject) (httpapi.ListSessionsResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ListSessions(w, r, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.ListSessions200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ListSessions400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ListSessions401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ListSessions403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ListSessions404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ListSessions405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ListSessions406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ListSessions409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ListSessions412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ListSessions413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ListSessions415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ListSessions422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ListSessions428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ListSessions429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ListSessions500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ListSessions503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ListSessions504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ListSessions 返回未声明状态 %d", w.status())
}

func (a *strictAPI) Login(ctx context.Context, request httpapi.LoginRequestObject) (httpapi.LoginResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.Login(w, r)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 201:
		return strictResponse[httpapi.Login201JSONResponse](w)
	case 400:
		return strictResponse[httpapi.Login400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.Login401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.Login403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.Login404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.Login405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.Login406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.Login409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.Login412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.Login413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.Login415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.Login422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.Login428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.Login429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.Login500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.Login503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.Login504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("Login 返回未声明状态 %d", w.status())
}

func (a *strictAPI) Logout(ctx context.Context, request httpapi.LogoutRequestObject) (httpapi.LogoutResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.Logout(w, r, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 204:
		return strictResponse[httpapi.Logout204Response](w)
	case 400:
		return strictResponse[httpapi.Logout400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.Logout401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.Logout403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.Logout404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.Logout405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.Logout406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.Logout409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.Logout412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.Logout413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.Logout415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.Logout422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.Logout428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.Logout429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.Logout500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.Logout503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.Logout504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("Logout 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetSession(ctx context.Context, request httpapi.GetSessionRequestObject) (httpapi.GetSessionResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetSession(w, r)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetSession200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetSession400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetSession401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetSession403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetSession404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetSession405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetSession406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetSession409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetSession412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetSession413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetSession415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetSession422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetSession428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetSession429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetSession500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetSession503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetSession504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetSession 返回未声明状态 %d", w.status())
}

func (a *strictAPI) RevokeSession(ctx context.Context, request httpapi.RevokeSessionRequestObject) (httpapi.RevokeSessionResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.RevokeSession(w, r, request.Id, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 204:
		return strictResponse[httpapi.RevokeSession204Response](w)
	case 400:
		return strictResponse[httpapi.RevokeSession400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.RevokeSession401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.RevokeSession403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.RevokeSession404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.RevokeSession405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.RevokeSession406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.RevokeSession409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.RevokeSession412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.RevokeSession413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.RevokeSession415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.RevokeSession422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.RevokeSession428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.RevokeSession429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.RevokeSession500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.RevokeSession503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.RevokeSession504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("RevokeSession 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ListComposeProjects(ctx context.Context, request httpapi.ListComposeProjectsRequestObject) (httpapi.ListComposeProjectsResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ListComposeProjects(w, r, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.ListComposeProjects200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ListComposeProjects400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ListComposeProjects401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ListComposeProjects403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ListComposeProjects404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ListComposeProjects405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ListComposeProjects406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ListComposeProjects409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ListComposeProjects412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ListComposeProjects413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ListComposeProjects415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ListComposeProjects422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ListComposeProjects428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ListComposeProjects429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ListComposeProjects500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ListComposeProjects503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ListComposeProjects504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ListComposeProjects 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ListThemes(ctx context.Context, request httpapi.ListThemesRequestObject) (httpapi.ListThemesResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ListThemes(w, r, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.ListThemes200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ListThemes400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ListThemes401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ListThemes403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ListThemes404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ListThemes405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ListThemes406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ListThemes409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ListThemes412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ListThemes413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ListThemes415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ListThemes422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ListThemes428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ListThemes429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ListThemes500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ListThemes503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ListThemes504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ListThemes 返回未声明状态 %d", w.status())
}

func (a *strictAPI) InstallTheme(ctx context.Context, request httpapi.InstallThemeRequestObject) (httpapi.InstallThemeResponseObject, error) {
	return nil, fmt.Errorf("流式接口由专用处理器执行")
}

func (a *strictAPI) DeleteTheme(ctx context.Context, request httpapi.DeleteThemeRequestObject) (httpapi.DeleteThemeResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.DeleteTheme(w, r, request.InstallationId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 204:
		return strictResponse[httpapi.DeleteTheme204Response](w)
	case 400:
		return strictResponse[httpapi.DeleteTheme400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.DeleteTheme401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.DeleteTheme403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.DeleteTheme404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.DeleteTheme405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.DeleteTheme406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.DeleteTheme409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.DeleteTheme412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.DeleteTheme413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.DeleteTheme415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.DeleteTheme422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.DeleteTheme428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.DeleteTheme429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.DeleteTheme500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.DeleteTheme503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.DeleteTheme504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("DeleteTheme 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetThemeInstallation(ctx context.Context, request httpapi.GetThemeInstallationRequestObject) (httpapi.GetThemeInstallationResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetThemeInstallation(w, r, request.InstallationId)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetThemeInstallation200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetThemeInstallation400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetThemeInstallation401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetThemeInstallation403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetThemeInstallation404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetThemeInstallation405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetThemeInstallation406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetThemeInstallation409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetThemeInstallation412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetThemeInstallation413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetThemeInstallation415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetThemeInstallation422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetThemeInstallation428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetThemeInstallation429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetThemeInstallation500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetThemeInstallation503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetThemeInstallation504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetThemeInstallation 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetThemeSelection(ctx context.Context, request httpapi.GetThemeSelectionRequestObject) (httpapi.GetThemeSelectionResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetThemeSelection(w, r)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetThemeSelection200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetThemeSelection400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetThemeSelection401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetThemeSelection403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetThemeSelection404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetThemeSelection405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetThemeSelection406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetThemeSelection409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetThemeSelection412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetThemeSelection413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetThemeSelection415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetThemeSelection422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetThemeSelection428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetThemeSelection429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetThemeSelection500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetThemeSelection503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetThemeSelection504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetThemeSelection 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ActivateTheme(ctx context.Context, request httpapi.ActivateThemeRequestObject) (httpapi.ActivateThemeResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ActivateTheme(w, r, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.ActivateTheme200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ActivateTheme400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ActivateTheme401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ActivateTheme403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ActivateTheme404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ActivateTheme405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ActivateTheme406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ActivateTheme409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ActivateTheme412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ActivateTheme413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ActivateTheme415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ActivateTheme422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ActivateTheme428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ActivateTheme429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ActivateTheme500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ActivateTheme503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ActivateTheme504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ActivateTheme 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ListContainers(ctx context.Context, request httpapi.ListContainersRequestObject) (httpapi.ListContainersResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ListContainers(w, r, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.ListContainers200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ListContainers400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ListContainers401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ListContainers403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ListContainers404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ListContainers405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ListContainers406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ListContainers409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ListContainers412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ListContainers413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ListContainers415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ListContainers422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ListContainers428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ListContainers429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ListContainers500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ListContainers503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ListContainers504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ListContainers 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ListDatabaseClusters(ctx context.Context, request httpapi.ListDatabaseClustersRequestObject) (httpapi.ListDatabaseClustersResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ListDatabaseClusters(w, r, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.ListDatabaseClusters200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ListDatabaseClusters400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ListDatabaseClusters401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ListDatabaseClusters403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ListDatabaseClusters404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ListDatabaseClusters405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ListDatabaseClusters406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ListDatabaseClusters409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ListDatabaseClusters412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ListDatabaseClusters413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ListDatabaseClusters415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ListDatabaseClusters422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ListDatabaseClusters428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ListDatabaseClusters429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ListDatabaseClusters500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ListDatabaseClusters503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ListDatabaseClusters504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ListDatabaseClusters 返回未声明状态 %d", w.status())
}

func (a *strictAPI) CreateDatabaseCluster(ctx context.Context, request httpapi.CreateDatabaseClusterRequestObject) (httpapi.CreateDatabaseClusterResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.CreateDatabaseCluster(w, r, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 201:
		return strictResponse[httpapi.CreateDatabaseCluster201JSONResponse](w)
	case 400:
		return strictResponse[httpapi.CreateDatabaseCluster400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.CreateDatabaseCluster401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.CreateDatabaseCluster403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.CreateDatabaseCluster404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.CreateDatabaseCluster405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.CreateDatabaseCluster406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.CreateDatabaseCluster409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.CreateDatabaseCluster412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.CreateDatabaseCluster413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.CreateDatabaseCluster415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.CreateDatabaseCluster422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.CreateDatabaseCluster428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.CreateDatabaseCluster429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.CreateDatabaseCluster500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.CreateDatabaseCluster503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.CreateDatabaseCluster504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("CreateDatabaseCluster 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetDatabaseDefinition(ctx context.Context, request httpapi.GetDatabaseDefinitionRequestObject) (httpapi.GetDatabaseDefinitionResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetDatabaseDefinition(w, r, request.Id)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetDatabaseDefinition200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetDatabaseDefinition400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetDatabaseDefinition401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetDatabaseDefinition403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetDatabaseDefinition404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetDatabaseDefinition405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetDatabaseDefinition406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetDatabaseDefinition409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetDatabaseDefinition412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetDatabaseDefinition413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetDatabaseDefinition415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetDatabaseDefinition422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetDatabaseDefinition428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetDatabaseDefinition429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetDatabaseDefinition500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetDatabaseDefinition503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetDatabaseDefinition504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetDatabaseDefinition 返回未声明状态 %d", w.status())
}

func (a *strictAPI) UpdateDatabaseCluster(ctx context.Context, request httpapi.UpdateDatabaseClusterRequestObject) (httpapi.UpdateDatabaseClusterResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.UpdateDatabaseCluster(w, r, request.Id, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.UpdateDatabaseCluster200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.UpdateDatabaseCluster400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.UpdateDatabaseCluster401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.UpdateDatabaseCluster403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.UpdateDatabaseCluster404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.UpdateDatabaseCluster405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.UpdateDatabaseCluster406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.UpdateDatabaseCluster409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.UpdateDatabaseCluster412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.UpdateDatabaseCluster413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.UpdateDatabaseCluster415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.UpdateDatabaseCluster422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.UpdateDatabaseCluster428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.UpdateDatabaseCluster429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.UpdateDatabaseCluster500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.UpdateDatabaseCluster503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.UpdateDatabaseCluster504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("UpdateDatabaseCluster 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetDatabaseBackups(ctx context.Context, request httpapi.GetDatabaseBackupsRequestObject) (httpapi.GetDatabaseBackupsResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetDatabaseBackups(w, r, request.Id)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetDatabaseBackups200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetDatabaseBackups400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetDatabaseBackups401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetDatabaseBackups403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetDatabaseBackups404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetDatabaseBackups405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetDatabaseBackups406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetDatabaseBackups409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetDatabaseBackups412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetDatabaseBackups413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetDatabaseBackups415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetDatabaseBackups422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetDatabaseBackups428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetDatabaseBackups429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetDatabaseBackups500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetDatabaseBackups503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetDatabaseBackups504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetDatabaseBackups 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ChangeDatabase(ctx context.Context, request httpapi.ChangeDatabaseRequestObject) (httpapi.ChangeDatabaseResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ChangeDatabase(w, r, request.Id, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 202:
		return strictResponse[httpapi.ChangeDatabase202JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ChangeDatabase400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ChangeDatabase401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ChangeDatabase403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ChangeDatabase404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ChangeDatabase405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ChangeDatabase406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ChangeDatabase409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ChangeDatabase412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ChangeDatabase413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ChangeDatabase415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ChangeDatabase422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ChangeDatabase428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ChangeDatabase429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ChangeDatabase500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ChangeDatabase503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ChangeDatabase504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ChangeDatabase 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetDatabaseCluster(ctx context.Context, request httpapi.GetDatabaseClusterRequestObject) (httpapi.GetDatabaseClusterResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetDatabaseCluster(w, r, request.Id, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetDatabaseCluster200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetDatabaseCluster400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetDatabaseCluster401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetDatabaseCluster403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetDatabaseCluster404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetDatabaseCluster405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetDatabaseCluster406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetDatabaseCluster409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetDatabaseCluster412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetDatabaseCluster413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetDatabaseCluster415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetDatabaseCluster422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetDatabaseCluster428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetDatabaseCluster429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetDatabaseCluster500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetDatabaseCluster503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetDatabaseCluster504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetDatabaseCluster 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetAPIDocumentation(ctx context.Context, request httpapi.GetAPIDocumentationRequestObject) (httpapi.GetAPIDocumentationResponseObject, error) {
	return nil, fmt.Errorf("流式接口由专用处理器执行")
}

func (a *strictAPI) ListEnrollmentTokens(ctx context.Context, request httpapi.ListEnrollmentTokensRequestObject) (httpapi.ListEnrollmentTokensResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ListEnrollmentTokens(w, r, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.ListEnrollmentTokens200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ListEnrollmentTokens400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ListEnrollmentTokens401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ListEnrollmentTokens403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ListEnrollmentTokens404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ListEnrollmentTokens405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ListEnrollmentTokens406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ListEnrollmentTokens409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ListEnrollmentTokens412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ListEnrollmentTokens413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ListEnrollmentTokens415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ListEnrollmentTokens422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ListEnrollmentTokens428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ListEnrollmentTokens429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ListEnrollmentTokens500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ListEnrollmentTokens503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ListEnrollmentTokens504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ListEnrollmentTokens 返回未声明状态 %d", w.status())
}

func (a *strictAPI) CreateEnrollmentToken(ctx context.Context, request httpapi.CreateEnrollmentTokenRequestObject) (httpapi.CreateEnrollmentTokenResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.CreateEnrollmentToken(w, r, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 201:
		return strictResponse[httpapi.CreateEnrollmentToken201JSONResponse](w)
	case 400:
		return strictResponse[httpapi.CreateEnrollmentToken400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.CreateEnrollmentToken401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.CreateEnrollmentToken403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.CreateEnrollmentToken404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.CreateEnrollmentToken405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.CreateEnrollmentToken406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.CreateEnrollmentToken409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.CreateEnrollmentToken412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.CreateEnrollmentToken413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.CreateEnrollmentToken415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.CreateEnrollmentToken422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.CreateEnrollmentToken428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.CreateEnrollmentToken429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.CreateEnrollmentToken500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.CreateEnrollmentToken503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.CreateEnrollmentToken504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("CreateEnrollmentToken 返回未声明状态 %d", w.status())
}

func (a *strictAPI) RevokeEnrollmentToken(ctx context.Context, request httpapi.RevokeEnrollmentTokenRequestObject) (httpapi.RevokeEnrollmentTokenResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.RevokeEnrollmentToken(w, r, request.Id, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 204:
		return strictResponse[httpapi.RevokeEnrollmentToken204Response](w)
	case 400:
		return strictResponse[httpapi.RevokeEnrollmentToken400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.RevokeEnrollmentToken401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.RevokeEnrollmentToken403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.RevokeEnrollmentToken404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.RevokeEnrollmentToken405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.RevokeEnrollmentToken406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.RevokeEnrollmentToken409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.RevokeEnrollmentToken412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.RevokeEnrollmentToken413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.RevokeEnrollmentToken415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.RevokeEnrollmentToken422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.RevokeEnrollmentToken428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.RevokeEnrollmentToken429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.RevokeEnrollmentToken500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.RevokeEnrollmentToken503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.RevokeEnrollmentToken504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("RevokeEnrollmentToken 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetEnrollmentToken(ctx context.Context, request httpapi.GetEnrollmentTokenRequestObject) (httpapi.GetEnrollmentTokenResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetEnrollmentToken(w, r, request.Id)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetEnrollmentToken200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetEnrollmentToken400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetEnrollmentToken401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetEnrollmentToken403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetEnrollmentToken404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetEnrollmentToken405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetEnrollmentToken406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetEnrollmentToken409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetEnrollmentToken412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetEnrollmentToken413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetEnrollmentToken415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetEnrollmentToken422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetEnrollmentToken428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetEnrollmentToken429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetEnrollmentToken500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetEnrollmentToken503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetEnrollmentToken504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetEnrollmentToken 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetEvents(ctx context.Context, request httpapi.GetEventsRequestObject) (httpapi.GetEventsResponseObject, error) {
	return nil, fmt.Errorf("流式接口由专用处理器执行")
}

func (a *strictAPI) GetMetrics(ctx context.Context, request httpapi.GetMetricsRequestObject) (httpapi.GetMetricsResponseObject, error) {
	return nil, fmt.Errorf("流式接口由专用处理器执行")
}

func (a *strictAPI) GetMonitoringCurrent(ctx context.Context, request httpapi.GetMonitoringCurrentRequestObject) (httpapi.GetMonitoringCurrentResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetMonitoringCurrent(w, r, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetMonitoringCurrent200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetMonitoringCurrent400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetMonitoringCurrent401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetMonitoringCurrent403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetMonitoringCurrent404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetMonitoringCurrent405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetMonitoringCurrent406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetMonitoringCurrent409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetMonitoringCurrent412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetMonitoringCurrent413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetMonitoringCurrent415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetMonitoringCurrent422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetMonitoringCurrent428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetMonitoringCurrent429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetMonitoringCurrent500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetMonitoringCurrent503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetMonitoringCurrent504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetMonitoringCurrent 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetMonitoringHistory(ctx context.Context, request httpapi.GetMonitoringHistoryRequestObject) (httpapi.GetMonitoringHistoryResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetMonitoringHistory(w, r, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetMonitoringHistory200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetMonitoringHistory400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetMonitoringHistory401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetMonitoringHistory403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetMonitoringHistory404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetMonitoringHistory405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetMonitoringHistory406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetMonitoringHistory409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetMonitoringHistory412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetMonitoringHistory413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetMonitoringHistory415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetMonitoringHistory422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetMonitoringHistory428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetMonitoringHistory429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetMonitoringHistory500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetMonitoringHistory503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetMonitoringHistory504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetMonitoringHistory 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ListNodes(ctx context.Context, request httpapi.ListNodesRequestObject) (httpapi.ListNodesResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ListNodes(w, r, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.ListNodes200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ListNodes400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ListNodes401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ListNodes403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ListNodes404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ListNodes405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ListNodes406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ListNodes409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ListNodes412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ListNodes413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ListNodes415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ListNodes422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ListNodes428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ListNodes429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ListNodes500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ListNodes503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ListNodes504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ListNodes 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetNode(ctx context.Context, request httpapi.GetNodeRequestObject) (httpapi.GetNodeResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetNode(w, r, request.NodeId)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetNode200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetNode400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetNode401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetNode403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetNode404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetNode405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetNode406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetNode409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetNode412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetNode413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetNode415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetNode422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetNode428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetNode429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetNode500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetNode503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetNode504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetNode 返回未声明状态 %d", w.status())
}

func (a *strictAPI) RenameNode(ctx context.Context, request httpapi.RenameNodeRequestObject) (httpapi.RenameNodeResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.RenameNode(w, r, request.NodeId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 202:
		return strictResponse[httpapi.RenameNode202JSONResponse](w)
	case 400:
		return strictResponse[httpapi.RenameNode400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.RenameNode401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.RenameNode403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.RenameNode404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.RenameNode405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.RenameNode406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.RenameNode409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.RenameNode412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.RenameNode413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.RenameNode415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.RenameNode422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.RenameNode428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.RenameNode429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.RenameNode500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.RenameNode503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.RenameNode504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("RenameNode 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetNodeAccount(ctx context.Context, request httpapi.GetNodeAccountRequestObject) (httpapi.GetNodeAccountResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetNodeAccount(w, r, request.NodeId)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetNodeAccount200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetNodeAccount400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetNodeAccount401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetNodeAccount403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetNodeAccount404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetNodeAccount405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetNodeAccount406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetNodeAccount409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetNodeAccount412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetNodeAccount413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetNodeAccount415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetNodeAccount422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetNodeAccount428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetNodeAccount429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetNodeAccount500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetNodeAccount503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetNodeAccount504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetNodeAccount 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ChangeNodeAccount(ctx context.Context, request httpapi.ChangeNodeAccountRequestObject) (httpapi.ChangeNodeAccountResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ChangeNodeAccount(w, r, request.NodeId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 202:
		return strictResponse[httpapi.ChangeNodeAccount202JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ChangeNodeAccount400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ChangeNodeAccount401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ChangeNodeAccount403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ChangeNodeAccount404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ChangeNodeAccount405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ChangeNodeAccount406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ChangeNodeAccount409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ChangeNodeAccount412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ChangeNodeAccount413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ChangeNodeAccount415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ChangeNodeAccount422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ChangeNodeAccount428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ChangeNodeAccount429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ChangeNodeAccount500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ChangeNodeAccount503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ChangeNodeAccount504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ChangeNodeAccount 返回未声明状态 %d", w.status())
}

func (a *strictAPI) RevealNodePassword(ctx context.Context, request httpapi.RevealNodePasswordRequestObject) (httpapi.RevealNodePasswordResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.RevealNodePassword(w, r, request.NodeId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.RevealNodePassword200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.RevealNodePassword400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.RevealNodePassword401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.RevealNodePassword403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.RevealNodePassword404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.RevealNodePassword405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.RevealNodePassword406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.RevealNodePassword409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.RevealNodePassword412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.RevealNodePassword413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.RevealNodePassword415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.RevealNodePassword422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.RevealNodePassword428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.RevealNodePassword429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.RevealNodePassword500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.RevealNodePassword503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.RevealNodePassword504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("RevealNodePassword 返回未声明状态 %d", w.status())
}

func (a *strictAPI) RevokeNode(ctx context.Context, request httpapi.RevokeNodeRequestObject) (httpapi.RevokeNodeResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.RevokeNode(w, r, request.NodeId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.RevokeNode200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.RevokeNode400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.RevokeNode401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.RevokeNode403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.RevokeNode404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.RevokeNode405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.RevokeNode406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.RevokeNode409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.RevokeNode412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.RevokeNode413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.RevokeNode415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.RevokeNode422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.RevokeNode428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.RevokeNode429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.RevokeNode500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.RevokeNode503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.RevokeNode504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("RevokeNode 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ListAPITokens(ctx context.Context, request httpapi.ListAPITokensRequestObject) (httpapi.ListAPITokensResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ListAPITokens(w, r, request.NodeId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.ListAPITokens200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ListAPITokens400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ListAPITokens401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ListAPITokens403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ListAPITokens404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ListAPITokens405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ListAPITokens406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ListAPITokens409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ListAPITokens412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ListAPITokens413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ListAPITokens415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ListAPITokens422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ListAPITokens428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ListAPITokens429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ListAPITokens500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ListAPITokens503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ListAPITokens504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ListAPITokens 返回未声明状态 %d", w.status())
}

func (a *strictAPI) CreateAPIToken(ctx context.Context, request httpapi.CreateAPITokenRequestObject) (httpapi.CreateAPITokenResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.CreateAPIToken(w, r, request.NodeId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 201:
		return strictResponse[httpapi.CreateAPIToken201JSONResponse](w)
	case 400:
		return strictResponse[httpapi.CreateAPIToken400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.CreateAPIToken401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.CreateAPIToken403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.CreateAPIToken404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.CreateAPIToken405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.CreateAPIToken406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.CreateAPIToken409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.CreateAPIToken412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.CreateAPIToken413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.CreateAPIToken415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.CreateAPIToken422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.CreateAPIToken428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.CreateAPIToken429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.CreateAPIToken500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.CreateAPIToken503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.CreateAPIToken504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("CreateAPIToken 返回未声明状态 %d", w.status())
}

func (a *strictAPI) DeleteAPIToken(ctx context.Context, request httpapi.DeleteAPITokenRequestObject) (httpapi.DeleteAPITokenResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.DeleteAPIToken(w, r, request.NodeId, request.Id, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 204:
		return strictResponse[httpapi.DeleteAPIToken204Response](w)
	case 400:
		return strictResponse[httpapi.DeleteAPIToken400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.DeleteAPIToken401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.DeleteAPIToken403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.DeleteAPIToken404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.DeleteAPIToken405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.DeleteAPIToken406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.DeleteAPIToken409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.DeleteAPIToken412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.DeleteAPIToken413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.DeleteAPIToken415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.DeleteAPIToken422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.DeleteAPIToken428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.DeleteAPIToken429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.DeleteAPIToken500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.DeleteAPIToken503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.DeleteAPIToken504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("DeleteAPIToken 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetAPIToken(ctx context.Context, request httpapi.GetAPITokenRequestObject) (httpapi.GetAPITokenResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetAPIToken(w, r, request.NodeId, request.Id)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetAPIToken200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetAPIToken400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetAPIToken401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetAPIToken403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetAPIToken404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetAPIToken405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetAPIToken406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetAPIToken409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetAPIToken412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetAPIToken413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetAPIToken415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetAPIToken422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetAPIToken428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetAPIToken429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetAPIToken500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetAPIToken503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetAPIToken504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetAPIToken 返回未声明状态 %d", w.status())
}

func (a *strictAPI) UpdateAPIToken(ctx context.Context, request httpapi.UpdateAPITokenRequestObject) (httpapi.UpdateAPITokenResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.UpdateAPIToken(w, r, request.NodeId, request.Id, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.UpdateAPIToken200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.UpdateAPIToken400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.UpdateAPIToken401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.UpdateAPIToken403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.UpdateAPIToken404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.UpdateAPIToken405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.UpdateAPIToken406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.UpdateAPIToken409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.UpdateAPIToken412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.UpdateAPIToken413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.UpdateAPIToken415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.UpdateAPIToken422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.UpdateAPIToken428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.UpdateAPIToken429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.UpdateAPIToken500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.UpdateAPIToken503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.UpdateAPIToken504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("UpdateAPIToken 返回未声明状态 %d", w.status())
}

func (a *strictAPI) RegenerateAPIToken(ctx context.Context, request httpapi.RegenerateAPITokenRequestObject) (httpapi.RegenerateAPITokenResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.RegenerateAPIToken(w, r, request.NodeId, request.Id, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.RegenerateAPIToken200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.RegenerateAPIToken400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.RegenerateAPIToken401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.RegenerateAPIToken403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.RegenerateAPIToken404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.RegenerateAPIToken405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.RegenerateAPIToken406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.RegenerateAPIToken409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.RegenerateAPIToken412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.RegenerateAPIToken413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.RegenerateAPIToken415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.RegenerateAPIToken422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.RegenerateAPIToken428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.RegenerateAPIToken429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.RegenerateAPIToken500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.RegenerateAPIToken503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.RegenerateAPIToken504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("RegenerateAPIToken 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetNodeSettings(ctx context.Context, request httpapi.GetNodeSettingsRequestObject) (httpapi.GetNodeSettingsResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetNodeSettings(w, r, request.NodeId)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetNodeSettings200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetNodeSettings400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetNodeSettings401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetNodeSettings403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetNodeSettings404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetNodeSettings405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetNodeSettings406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetNodeSettings409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetNodeSettings412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetNodeSettings413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetNodeSettings415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetNodeSettings422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetNodeSettings428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetNodeSettings429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetNodeSettings500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetNodeSettings503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetNodeSettings504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetNodeSettings 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ChangeNodeSettings(ctx context.Context, request httpapi.ChangeNodeSettingsRequestObject) (httpapi.ChangeNodeSettingsResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ChangeNodeSettings(w, r, request.NodeId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 202:
		return strictResponse[httpapi.ChangeNodeSettings202JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ChangeNodeSettings400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ChangeNodeSettings401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ChangeNodeSettings403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ChangeNodeSettings404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ChangeNodeSettings405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ChangeNodeSettings406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ChangeNodeSettings409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ChangeNodeSettings412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ChangeNodeSettings413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ChangeNodeSettings415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ChangeNodeSettings422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ChangeNodeSettings428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ChangeNodeSettings429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ChangeNodeSettings500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ChangeNodeSettings503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ChangeNodeSettings504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ChangeNodeSettings 返回未声明状态 %d", w.status())
}

func (a *strictAPI) DiscoverDatabases(ctx context.Context, request httpapi.DiscoverDatabasesRequestObject) (httpapi.DiscoverDatabasesResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.DiscoverDatabases(w, r, request.NodeId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.DiscoverDatabases200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.DiscoverDatabases400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.DiscoverDatabases401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.DiscoverDatabases403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.DiscoverDatabases404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.DiscoverDatabases405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.DiscoverDatabases406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.DiscoverDatabases409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.DiscoverDatabases412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.DiscoverDatabases413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.DiscoverDatabases415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.DiscoverDatabases422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.DiscoverDatabases428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.DiscoverDatabases429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.DiscoverDatabases500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.DiscoverDatabases503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.DiscoverDatabases504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("DiscoverDatabases 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetDatabaseInstance(ctx context.Context, request httpapi.GetDatabaseInstanceRequestObject) (httpapi.GetDatabaseInstanceResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetDatabaseInstance(w, r, request.NodeId, request.InstanceId)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetDatabaseInstance200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetDatabaseInstance400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetDatabaseInstance401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetDatabaseInstance403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetDatabaseInstance404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetDatabaseInstance405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetDatabaseInstance406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetDatabaseInstance409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetDatabaseInstance412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetDatabaseInstance413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetDatabaseInstance415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetDatabaseInstance422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetDatabaseInstance428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetDatabaseInstance429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetDatabaseInstance500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetDatabaseInstance503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetDatabaseInstance504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetDatabaseInstance 返回未声明状态 %d", w.status())
}

func (a *strictAPI) SaveDatabaseInstance(ctx context.Context, request httpapi.SaveDatabaseInstanceRequestObject) (httpapi.SaveDatabaseInstanceResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.SaveDatabaseInstance(w, r, request.NodeId, request.InstanceId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.SaveDatabaseInstance200JSONResponse](w)
	case 201:
		return strictResponse[httpapi.SaveDatabaseInstance201JSONResponse](w)
	case 400:
		return strictResponse[httpapi.SaveDatabaseInstance400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.SaveDatabaseInstance401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.SaveDatabaseInstance403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.SaveDatabaseInstance404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.SaveDatabaseInstance405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.SaveDatabaseInstance406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.SaveDatabaseInstance409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.SaveDatabaseInstance412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.SaveDatabaseInstance413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.SaveDatabaseInstance415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.SaveDatabaseInstance422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.SaveDatabaseInstance428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.SaveDatabaseInstance429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.SaveDatabaseInstance500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.SaveDatabaseInstance503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.SaveDatabaseInstance504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("SaveDatabaseInstance 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetComposeProject(ctx context.Context, request httpapi.GetComposeProjectRequestObject) (httpapi.GetComposeProjectResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetComposeProject(w, r, request.NodeId, request.EngineId, request.ProjectId)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetComposeProject200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetComposeProject400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetComposeProject401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetComposeProject403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetComposeProject404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetComposeProject405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetComposeProject406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetComposeProject409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetComposeProject412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetComposeProject413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetComposeProject415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetComposeProject422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetComposeProject428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetComposeProject429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetComposeProject500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetComposeProject503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetComposeProject504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetComposeProject 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ChangeComposeProject(ctx context.Context, request httpapi.ChangeComposeProjectRequestObject) (httpapi.ChangeComposeProjectResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ChangeComposeProject(w, r, request.NodeId, request.EngineId, request.ProjectId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 202:
		return strictResponse[httpapi.ChangeComposeProject202JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ChangeComposeProject400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ChangeComposeProject401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ChangeComposeProject403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ChangeComposeProject404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ChangeComposeProject405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ChangeComposeProject406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ChangeComposeProject409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ChangeComposeProject412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ChangeComposeProject413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ChangeComposeProject415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ChangeComposeProject422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ChangeComposeProject428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ChangeComposeProject429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ChangeComposeProject500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ChangeComposeProject503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ChangeComposeProject504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ChangeComposeProject 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetComposeRegistration(ctx context.Context, request httpapi.GetComposeRegistrationRequestObject) (httpapi.GetComposeRegistrationResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetComposeRegistration(w, r, request.NodeId, request.EngineId, request.ProjectId)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetComposeRegistration200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetComposeRegistration400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetComposeRegistration401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetComposeRegistration403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetComposeRegistration404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetComposeRegistration405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetComposeRegistration406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetComposeRegistration409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetComposeRegistration412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetComposeRegistration413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetComposeRegistration415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetComposeRegistration422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetComposeRegistration428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetComposeRegistration429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetComposeRegistration500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetComposeRegistration503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetComposeRegistration504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetComposeRegistration 返回未声明状态 %d", w.status())
}

func (a *strictAPI) RegisterComposeProject(ctx context.Context, request httpapi.RegisterComposeProjectRequestObject) (httpapi.RegisterComposeProjectResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.RegisterComposeProject(w, r, request.NodeId, request.EngineId, request.ProjectId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 202:
		return strictResponse[httpapi.RegisterComposeProject202JSONResponse](w)
	case 400:
		return strictResponse[httpapi.RegisterComposeProject400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.RegisterComposeProject401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.RegisterComposeProject403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.RegisterComposeProject404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.RegisterComposeProject405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.RegisterComposeProject406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.RegisterComposeProject409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.RegisterComposeProject412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.RegisterComposeProject413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.RegisterComposeProject415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.RegisterComposeProject422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.RegisterComposeProject428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.RegisterComposeProject429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.RegisterComposeProject500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.RegisterComposeProject503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.RegisterComposeProject504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("RegisterComposeProject 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetContainer(ctx context.Context, request httpapi.GetContainerRequestObject) (httpapi.GetContainerResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetContainer(w, r, request.NodeId, request.EngineId, request.ContainerId)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetContainer200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetContainer400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetContainer401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetContainer403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetContainer404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetContainer405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetContainer406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetContainer409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetContainer412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetContainer413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetContainer415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetContainer422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetContainer428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetContainer429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetContainer500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetContainer503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetContainer504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetContainer 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ChangeContainer(ctx context.Context, request httpapi.ChangeContainerRequestObject) (httpapi.ChangeContainerResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ChangeContainer(w, r, request.NodeId, request.EngineId, request.ContainerId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 202:
		return strictResponse[httpapi.ChangeContainer202JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ChangeContainer400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ChangeContainer401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ChangeContainer403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ChangeContainer404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ChangeContainer405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ChangeContainer406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ChangeContainer409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ChangeContainer412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ChangeContainer413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ChangeContainer415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ChangeContainer422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ChangeContainer428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ChangeContainer429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ChangeContainer500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ChangeContainer503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ChangeContainer504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ChangeContainer 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetContainerLogs(ctx context.Context, request httpapi.GetContainerLogsRequestObject) (httpapi.GetContainerLogsResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetContainerLogs(w, r, request.NodeId, request.EngineId, request.ContainerId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetContainerLogs200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetContainerLogs400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetContainerLogs401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetContainerLogs403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetContainerLogs404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetContainerLogs405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetContainerLogs406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetContainerLogs409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetContainerLogs412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetContainerLogs413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetContainerLogs415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetContainerLogs422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetContainerLogs428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetContainerLogs429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetContainerLogs500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetContainerLogs503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetContainerLogs504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetContainerLogs 返回未声明状态 %d", w.status())
}

func (a *strictAPI) FirewallAction(ctx context.Context, request httpapi.FirewallActionRequestObject) (httpapi.FirewallActionResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.FirewallAction(w, r, request.NodeId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 202:
		return strictResponse[httpapi.FirewallAction202JSONResponse](w)
	case 400:
		return strictResponse[httpapi.FirewallAction400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.FirewallAction401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.FirewallAction403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.FirewallAction404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.FirewallAction405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.FirewallAction406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.FirewallAction409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.FirewallAction412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.FirewallAction413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.FirewallAction415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.FirewallAction422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.FirewallAction428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.FirewallAction429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.FirewallAction500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.FirewallAction503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.FirewallAction504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("FirewallAction 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ChangeFirewallConfig(ctx context.Context, request httpapi.ChangeFirewallConfigRequestObject) (httpapi.ChangeFirewallConfigResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ChangeFirewallConfig(w, r, request.NodeId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 202:
		return strictResponse[httpapi.ChangeFirewallConfig202JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ChangeFirewallConfig400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ChangeFirewallConfig401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ChangeFirewallConfig403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ChangeFirewallConfig404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ChangeFirewallConfig405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ChangeFirewallConfig406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ChangeFirewallConfig409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ChangeFirewallConfig412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ChangeFirewallConfig413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ChangeFirewallConfig415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ChangeFirewallConfig422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ChangeFirewallConfig428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ChangeFirewallConfig429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ChangeFirewallConfig500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ChangeFirewallConfig503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ChangeFirewallConfig504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ChangeFirewallConfig 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ReadFirewall(ctx context.Context, request httpapi.ReadFirewallRequestObject) (httpapi.ReadFirewallResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ReadFirewall(w, r, request.NodeId)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.ReadFirewall200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ReadFirewall400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ReadFirewall401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ReadFirewall403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ReadFirewall404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ReadFirewall405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ReadFirewall406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ReadFirewall409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ReadFirewall412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ReadFirewall413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ReadFirewall415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ReadFirewall422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ReadFirewall428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ReadFirewall429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ReadFirewall500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ReadFirewall503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ReadFirewall504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ReadFirewall 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetNodePeerTrust(ctx context.Context, request httpapi.GetNodePeerTrustRequestObject) (httpapi.GetNodePeerTrustResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetNodePeerTrust(w, r, request.NodeId)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetNodePeerTrust200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetNodePeerTrust400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetNodePeerTrust401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetNodePeerTrust403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetNodePeerTrust404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetNodePeerTrust405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetNodePeerTrust406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetNodePeerTrust409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetNodePeerTrust412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetNodePeerTrust413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetNodePeerTrust415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetNodePeerTrust422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetNodePeerTrust428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetNodePeerTrust429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetNodePeerTrust500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetNodePeerTrust503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetNodePeerTrust504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetNodePeerTrust 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetFirewallStatus(ctx context.Context, request httpapi.GetFirewallStatusRequestObject) (httpapi.GetFirewallStatusResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetFirewallStatus(w, r, request.NodeId)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetFirewallStatus200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetFirewallStatus400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetFirewallStatus401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetFirewallStatus403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetFirewallStatus404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetFirewallStatus405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetFirewallStatus406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetFirewallStatus409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetFirewallStatus412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetFirewallStatus413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetFirewallStatus415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetFirewallStatus422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetFirewallStatus428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetFirewallStatus429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetFirewallStatus500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetFirewallStatus503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetFirewallStatus504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetFirewallStatus 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetPublicAddresses(ctx context.Context, request httpapi.GetPublicAddressesRequestObject) (httpapi.GetPublicAddressesResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetPublicAddresses(w, r, request.NodeId)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetPublicAddresses200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetPublicAddresses400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetPublicAddresses401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetPublicAddresses403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetPublicAddresses404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetPublicAddresses405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetPublicAddresses406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetPublicAddresses409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetPublicAddresses412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetPublicAddresses413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetPublicAddresses415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetPublicAddresses422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetPublicAddresses428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetPublicAddresses429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetPublicAddresses500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetPublicAddresses503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetPublicAddresses504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetPublicAddresses 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ChangePublicAddresses(ctx context.Context, request httpapi.ChangePublicAddressesRequestObject) (httpapi.ChangePublicAddressesResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ChangePublicAddresses(w, r, request.NodeId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.ChangePublicAddresses200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ChangePublicAddresses400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ChangePublicAddresses401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ChangePublicAddresses403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ChangePublicAddresses404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ChangePublicAddresses405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ChangePublicAddresses406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ChangePublicAddresses409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ChangePublicAddresses412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ChangePublicAddresses413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ChangePublicAddresses415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ChangePublicAddresses422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ChangePublicAddresses428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ChangePublicAddresses429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ChangePublicAddresses500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ChangePublicAddresses503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ChangePublicAddresses504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ChangePublicAddresses 返回未声明状态 %d", w.status())
}

func (a *strictAPI) RefreshPublicAddresses(ctx context.Context, request httpapi.RefreshPublicAddressesRequestObject) (httpapi.RefreshPublicAddressesResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.RefreshPublicAddresses(w, r, request.NodeId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.RefreshPublicAddresses200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.RefreshPublicAddresses400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.RefreshPublicAddresses401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.RefreshPublicAddresses403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.RefreshPublicAddresses404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.RefreshPublicAddresses405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.RefreshPublicAddresses406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.RefreshPublicAddresses409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.RefreshPublicAddresses412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.RefreshPublicAddresses413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.RefreshPublicAddresses415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.RefreshPublicAddresses422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.RefreshPublicAddresses428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.RefreshPublicAddresses429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.RefreshPublicAddresses500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.RefreshPublicAddresses503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.RefreshPublicAddresses504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("RefreshPublicAddresses 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetNodeStatus(ctx context.Context, request httpapi.GetNodeStatusRequestObject) (httpapi.GetNodeStatusResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetNodeStatus(w, r, request.NodeId)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetNodeStatus200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetNodeStatus400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetNodeStatus401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetNodeStatus403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetNodeStatus404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetNodeStatus405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetNodeStatus406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetNodeStatus409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetNodeStatus412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetNodeStatus413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetNodeStatus415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetNodeStatus422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetNodeStatus428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetNodeStatus429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetNodeStatus500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetNodeStatus503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetNodeStatus504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetNodeStatus 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetStorageStatus(ctx context.Context, request httpapi.GetStorageStatusRequestObject) (httpapi.GetStorageStatusResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetStorageStatus(w, r, request.NodeId)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetStorageStatus200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetStorageStatus400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetStorageStatus401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetStorageStatus403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetStorageStatus404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetStorageStatus405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetStorageStatus406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetStorageStatus409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetStorageStatus412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetStorageStatus413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetStorageStatus415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetStorageStatus422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetStorageStatus428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetStorageStatus429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetStorageStatus500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetStorageStatus503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetStorageStatus504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetStorageStatus 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetScheduledTask(ctx context.Context, request httpapi.GetScheduledTaskRequestObject) (httpapi.GetScheduledTaskResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetScheduledTask(w, r, request.NodeId, request.TaskId)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetScheduledTask200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetScheduledTask400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetScheduledTask401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetScheduledTask403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetScheduledTask404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetScheduledTask405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetScheduledTask406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetScheduledTask409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetScheduledTask412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetScheduledTask413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetScheduledTask415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetScheduledTask422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetScheduledTask428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetScheduledTask429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetScheduledTask500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetScheduledTask503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetScheduledTask504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetScheduledTask 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetScheduledTaskDefinition(ctx context.Context, request httpapi.GetScheduledTaskDefinitionRequestObject) (httpapi.GetScheduledTaskDefinitionResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetScheduledTaskDefinition(w, r, request.NodeId, request.TaskId)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetScheduledTaskDefinition200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetScheduledTaskDefinition400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetScheduledTaskDefinition401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetScheduledTaskDefinition403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetScheduledTaskDefinition404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetScheduledTaskDefinition405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetScheduledTaskDefinition406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetScheduledTaskDefinition409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetScheduledTaskDefinition412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetScheduledTaskDefinition413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetScheduledTaskDefinition415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetScheduledTaskDefinition422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetScheduledTaskDefinition428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetScheduledTaskDefinition429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetScheduledTaskDefinition500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetScheduledTaskDefinition503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetScheduledTaskDefinition504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetScheduledTaskDefinition 返回未声明状态 %d", w.status())
}

func (a *strictAPI) SaveScheduledTask(ctx context.Context, request httpapi.SaveScheduledTaskRequestObject) (httpapi.SaveScheduledTaskResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.SaveScheduledTask(w, r, request.NodeId, request.TaskId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 202:
		return strictResponse[httpapi.SaveScheduledTask202JSONResponse](w)
	case 400:
		return strictResponse[httpapi.SaveScheduledTask400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.SaveScheduledTask401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.SaveScheduledTask403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.SaveScheduledTask404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.SaveScheduledTask405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.SaveScheduledTask406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.SaveScheduledTask409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.SaveScheduledTask412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.SaveScheduledTask413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.SaveScheduledTask415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.SaveScheduledTask422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.SaveScheduledTask428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.SaveScheduledTask429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.SaveScheduledTask500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.SaveScheduledTask503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.SaveScheduledTask504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("SaveScheduledTask 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ListScheduledTaskRuns(ctx context.Context, request httpapi.ListScheduledTaskRunsRequestObject) (httpapi.ListScheduledTaskRunsResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ListScheduledTaskRuns(w, r, request.NodeId, request.TaskId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.ListScheduledTaskRuns200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ListScheduledTaskRuns400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ListScheduledTaskRuns401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ListScheduledTaskRuns403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ListScheduledTaskRuns404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ListScheduledTaskRuns405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ListScheduledTaskRuns406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ListScheduledTaskRuns409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ListScheduledTaskRuns412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ListScheduledTaskRuns413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ListScheduledTaskRuns415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ListScheduledTaskRuns422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ListScheduledTaskRuns428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ListScheduledTaskRuns429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ListScheduledTaskRuns500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ListScheduledTaskRuns503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ListScheduledTaskRuns504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ListScheduledTaskRuns 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ExecuteScheduledTask(ctx context.Context, request httpapi.ExecuteScheduledTaskRequestObject) (httpapi.ExecuteScheduledTaskResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ExecuteScheduledTask(w, r, request.NodeId, request.TaskId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 202:
		return strictResponse[httpapi.ExecuteScheduledTask202JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ExecuteScheduledTask400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ExecuteScheduledTask401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ExecuteScheduledTask403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ExecuteScheduledTask404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ExecuteScheduledTask405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ExecuteScheduledTask406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ExecuteScheduledTask409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ExecuteScheduledTask412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ExecuteScheduledTask413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ExecuteScheduledTask415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ExecuteScheduledTask422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ExecuteScheduledTask428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ExecuteScheduledTask429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ExecuteScheduledTask500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ExecuteScheduledTask503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ExecuteScheduledTask504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ExecuteScheduledTask 返回未声明状态 %d", w.status())
}

func (a *strictAPI) CreateNodeTerminal(ctx context.Context, request httpapi.CreateNodeTerminalRequestObject) (httpapi.CreateNodeTerminalResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.CreateNodeTerminal(w, r, request.NodeId, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 201:
		return strictResponse[httpapi.CreateNodeTerminal201JSONResponse](w)
	case 400:
		return strictResponse[httpapi.CreateNodeTerminal400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.CreateNodeTerminal401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.CreateNodeTerminal403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.CreateNodeTerminal404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.CreateNodeTerminal405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.CreateNodeTerminal406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.CreateNodeTerminal409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.CreateNodeTerminal412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.CreateNodeTerminal413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.CreateNodeTerminal415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.CreateNodeTerminal422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.CreateNodeTerminal428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.CreateNodeTerminal429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.CreateNodeTerminal500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.CreateNodeTerminal503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.CreateNodeTerminal504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("CreateNodeTerminal 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetOpenAPISpec(ctx context.Context, request httpapi.GetOpenAPISpecRequestObject) (httpapi.GetOpenAPISpecResponseObject, error) {
	return nil, fmt.Errorf("流式接口由专用处理器执行")
}

func (a *strictAPI) ListOperations(ctx context.Context, request httpapi.ListOperationsRequestObject) (httpapi.ListOperationsResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ListOperations(w, r, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.ListOperations200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ListOperations400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ListOperations401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ListOperations403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ListOperations404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ListOperations405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ListOperations406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ListOperations409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ListOperations412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ListOperations413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ListOperations415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ListOperations422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ListOperations428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ListOperations429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ListOperations500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ListOperations503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ListOperations504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ListOperations 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetOperation(ctx context.Context, request httpapi.GetOperationRequestObject) (httpapi.GetOperationResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetOperation(w, r, request.Id)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetOperation200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetOperation400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetOperation401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetOperation403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetOperation404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetOperation405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetOperation406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetOperation409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetOperation412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetOperation413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetOperation415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetOperation422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetOperation428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetOperation429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetOperation500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetOperation503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetOperation504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetOperation 返回未声明状态 %d", w.status())
}

func (a *strictAPI) DecideOperation(ctx context.Context, request httpapi.DecideOperationRequestObject) (httpapi.DecideOperationResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.DecideOperation(w, r, request.Id, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.DecideOperation200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.DecideOperation400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.DecideOperation401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.DecideOperation403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.DecideOperation404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.DecideOperation405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.DecideOperation406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.DecideOperation409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.DecideOperation412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.DecideOperation413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.DecideOperation415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.DecideOperation422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.DecideOperation428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.DecideOperation429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.DecideOperation500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.DecideOperation503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.DecideOperation504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("DecideOperation 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ReadExecutionLogs(ctx context.Context, request httpapi.ReadExecutionLogsRequestObject) (httpapi.ReadExecutionLogsResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ReadExecutionLogs(w, r, request.Id, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.ReadExecutionLogs200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ReadExecutionLogs400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ReadExecutionLogs401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ReadExecutionLogs403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ReadExecutionLogs404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ReadExecutionLogs405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ReadExecutionLogs406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ReadExecutionLogs409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ReadExecutionLogs412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ReadExecutionLogs413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ReadExecutionLogs415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ReadExecutionLogs422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ReadExecutionLogs428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ReadExecutionLogs429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ReadExecutionLogs500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ReadExecutionLogs503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ReadExecutionLogs504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ReadExecutionLogs 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetOverview(ctx context.Context, request httpapi.GetOverviewRequestObject) (httpapi.GetOverviewResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetOverview(w, r)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetOverview200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetOverview400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetOverview401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetOverview403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetOverview404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetOverview405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetOverview406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetOverview409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetOverview412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetOverview413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetOverview415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetOverview422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetOverview428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetOverview429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetOverview500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetOverview503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetOverview504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetOverview 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetPeerTrust(ctx context.Context, request httpapi.GetPeerTrustRequestObject) (httpapi.GetPeerTrustResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetPeerTrust(w, r)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetPeerTrust200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetPeerTrust400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetPeerTrust401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetPeerTrust403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetPeerTrust404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetPeerTrust405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetPeerTrust406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetPeerTrust409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetPeerTrust412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetPeerTrust413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetPeerTrust415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetPeerTrust422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetPeerTrust428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetPeerTrust429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetPeerTrust500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetPeerTrust503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetPeerTrust504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetPeerTrust 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ChangePeerTrust(ctx context.Context, request httpapi.ChangePeerTrustRequestObject) (httpapi.ChangePeerTrustResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ChangePeerTrust(w, r, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.ChangePeerTrust200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ChangePeerTrust400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ChangePeerTrust401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ChangePeerTrust403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ChangePeerTrust404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ChangePeerTrust405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ChangePeerTrust406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ChangePeerTrust409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ChangePeerTrust412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ChangePeerTrust413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ChangePeerTrust415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ChangePeerTrust422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ChangePeerTrust428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ChangePeerTrust429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ChangePeerTrust500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ChangePeerTrust503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ChangePeerTrust504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ChangePeerTrust 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetPeerTrustStatus(ctx context.Context, request httpapi.GetPeerTrustStatusRequestObject) (httpapi.GetPeerTrustStatusResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetPeerTrustStatus(w, r)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetPeerTrustStatus200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetPeerTrustStatus400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetPeerTrustStatus401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetPeerTrustStatus403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetPeerTrustStatus404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetPeerTrustStatus405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetPeerTrustStatus406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetPeerTrustStatus409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetPeerTrustStatus412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetPeerTrustStatus413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetPeerTrustStatus415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetPeerTrustStatus422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetPeerTrustStatus428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetPeerTrustStatus429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetPeerTrustStatus500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetPeerTrustStatus503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetPeerTrustStatus504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetPeerTrustStatus 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetRequestReceipt(ctx context.Context, request httpapi.GetRequestReceiptRequestObject) (httpapi.GetRequestReceiptResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetRequestReceipt(w, r, request.RequestKey, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetRequestReceipt200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetRequestReceipt400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetRequestReceipt401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetRequestReceipt403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetRequestReceipt404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetRequestReceipt405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetRequestReceipt406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetRequestReceipt409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetRequestReceipt412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetRequestReceipt413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetRequestReceipt415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetRequestReceipt422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetRequestReceipt428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetRequestReceipt429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetRequestReceipt500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetRequestReceipt503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetRequestReceipt504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetRequestReceipt 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetAccount(ctx context.Context, request httpapi.GetAccountRequestObject) (httpapi.GetAccountResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetAccount(w, r)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetAccount200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetAccount400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetAccount401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetAccount403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetAccount404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetAccount405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetAccount406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetAccount409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetAccount412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetAccount413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetAccount415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetAccount422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetAccount428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetAccount429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetAccount500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetAccount503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetAccount504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetAccount 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ChangeAccount(ctx context.Context, request httpapi.ChangeAccountRequestObject) (httpapi.ChangeAccountResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ChangeAccount(w, r, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.ChangeAccount200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ChangeAccount400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ChangeAccount401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ChangeAccount403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ChangeAccount404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ChangeAccount405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ChangeAccount406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ChangeAccount409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ChangeAccount412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ChangeAccount413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ChangeAccount415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ChangeAccount422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ChangeAccount428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ChangeAccount429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ChangeAccount500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ChangeAccount503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ChangeAccount504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ChangeAccount 返回未声明状态 %d", w.status())
}

func (a *strictAPI) GetNavigation(ctx context.Context, request httpapi.GetNavigationRequestObject) (httpapi.GetNavigationResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.GetNavigation(w, r)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.GetNavigation200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.GetNavigation400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.GetNavigation401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.GetNavigation403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.GetNavigation404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.GetNavigation405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.GetNavigation406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.GetNavigation409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.GetNavigation412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.GetNavigation413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.GetNavigation415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.GetNavigation422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.GetNavigation428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.GetNavigation429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.GetNavigation500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.GetNavigation503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.GetNavigation504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("GetNavigation 返回未声明状态 %d", w.status())
}

func (a *strictAPI) UpdateNavigation(ctx context.Context, request httpapi.UpdateNavigationRequestObject) (httpapi.UpdateNavigationResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.UpdateNavigation(w, r, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.UpdateNavigation200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.UpdateNavigation400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.UpdateNavigation401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.UpdateNavigation403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.UpdateNavigation404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.UpdateNavigation405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.UpdateNavigation406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.UpdateNavigation409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.UpdateNavigation412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.UpdateNavigation413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.UpdateNavigation415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.UpdateNavigation422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.UpdateNavigation428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.UpdateNavigation429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.UpdateNavigation500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.UpdateNavigation503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.UpdateNavigation504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("UpdateNavigation 返回未声明状态 %d", w.status())
}

func (a *strictAPI) ListScheduledTasks(ctx context.Context, request httpapi.ListScheduledTasksRequestObject) (httpapi.ListScheduledTasksResponseObject, error) {
	r := strictRequest(ctx)
	w := newAPIResponse(r)
	a.server.ListScheduledTasks(w, r, request.Params)
	a.server.prepareAPIResponse(w, r)
	switch w.status() {
	case 200:
		return strictResponse[httpapi.ListScheduledTasks200JSONResponse](w)
	case 400:
		return strictResponse[httpapi.ListScheduledTasks400ApplicationProblemPlusJSONResponse](w)
	case 401:
		return strictResponse[httpapi.ListScheduledTasks401ApplicationProblemPlusJSONResponse](w)
	case 403:
		return strictResponse[httpapi.ListScheduledTasks403ApplicationProblemPlusJSONResponse](w)
	case 404:
		return strictResponse[httpapi.ListScheduledTasks404ApplicationProblemPlusJSONResponse](w)
	case 405:
		return strictResponse[httpapi.ListScheduledTasks405ApplicationProblemPlusJSONResponse](w)
	case 406:
		return strictResponse[httpapi.ListScheduledTasks406ApplicationProblemPlusJSONResponse](w)
	case 409:
		return strictResponse[httpapi.ListScheduledTasks409ApplicationProblemPlusJSONResponse](w)
	case 412:
		return strictResponse[httpapi.ListScheduledTasks412ApplicationProblemPlusJSONResponse](w)
	case 413:
		return strictResponse[httpapi.ListScheduledTasks413ApplicationProblemPlusJSONResponse](w)
	case 415:
		return strictResponse[httpapi.ListScheduledTasks415ApplicationProblemPlusJSONResponse](w)
	case 422:
		return strictResponse[httpapi.ListScheduledTasks422ApplicationProblemPlusJSONResponse](w)
	case 428:
		return strictResponse[httpapi.ListScheduledTasks428ApplicationProblemPlusJSONResponse](w)
	case 429:
		return strictResponse[httpapi.ListScheduledTasks429ApplicationProblemPlusJSONResponse](w)
	case 500:
		return strictResponse[httpapi.ListScheduledTasks500ApplicationProblemPlusJSONResponse](w)
	case 503:
		return strictResponse[httpapi.ListScheduledTasks503ApplicationProblemPlusJSONResponse](w)
	case 504:
		return strictResponse[httpapi.ListScheduledTasks504ApplicationProblemPlusJSONResponse](w)
	}
	return nil, fmt.Errorf("ListScheduledTasks 返回未声明状态 %d", w.status())
}

func (a *strictAPI) AttachTerminal(ctx context.Context, request httpapi.AttachTerminalRequestObject) (httpapi.AttachTerminalResponseObject, error) {
	return nil, fmt.Errorf("流式接口由专用处理器执行")
}
