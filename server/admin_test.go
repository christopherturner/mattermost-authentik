package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func setupTestPluginWithAPI() (*Plugin, *plugintest.API) {
	p := &Plugin{}
	api := &plugintest.API{}
	p.SetAPI(api)
	p.setupRouter()
	return p, api
}

func TestAdminEndpoints_AuthorizationCheck(t *testing.T) {
	p, api := setupTestPluginWithAPI()

	// Non-admin user
	api.On("GetUser", "non-admin-id").Return(&model.User{
		Id:    "non-admin-id",
		Roles: model.SystemUserRoleId,
	}, nil)

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/admin/ldap-users"},
		{http.MethodPost, "/api/v1/admin/unlock-ldap"},
		{http.MethodPost, "/api/v1/admin/deactivate-ldap"},
		{http.MethodPost, "/api/v1/admin/diagnose"},
	}

	for _, ep := range endpoints {
		t.Run("missing user ID on "+ep.path, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, strings.NewReader("{}"))
			w := httptest.NewRecorder()
			p.router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusForbidden, w.Code)
		})

		t.Run("non-admin user on "+ep.path, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, strings.NewReader("{}"))
			req.Header.Set("Mattermost-User-Id", "non-admin-id")
			w := httptest.NewRecorder()
			p.router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusForbidden, w.Code)
		})
	}
}

func TestHandleGetLDAPUsers(t *testing.T) {
	p, api := setupTestPluginWithAPI()

	adminUser := &model.User{
		Id:    "admin-user-id",
		Roles: model.SystemAdminRoleId + " " + model.SystemUserRoleId,
	}
	api.On("GetUser", "admin-user-id").Return(adminUser, nil)

	ldapUser1 := &model.User{
		Id:          "u1",
		Username:    "ldap.one",
		Email:       "ldap.one@example.com",
		FirstName:   "LDAP",
		LastName:    "One",
		AuthService: "ldap",
		DeleteAt:    0,
	}
	ldapUserDeleted := &model.User{
		Id:          "u2",
		Username:    "ldap.deleted",
		Email:       "ldap.deleted@example.com",
		AuthService: "ldap",
		DeleteAt:    1234567, // deactivated
	}
	nativeUser := &model.User{
		Id:          "u3",
		Username:    "native.user",
		Email:       "native@example.com",
		AuthService: "",
		DeleteAt:    0,
	}

	api.On("GetUsers", mock.MatchedBy(func(opts *model.UserGetOptions) bool {
		return opts.Page == 0
	})).Return([]*model.User{ldapUser1, ldapUserDeleted, nativeUser}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ldap-users", nil)
	req.Header.Set("Mattermost-User-Id", "admin-user-id")
	w := httptest.NewRecorder()

	p.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Count int                      `json:"count"`
		Users []map[string]interface{} `json:"users"`
	}
	err := json.NewDecoder(w.Body).Decode(&resp)
	assert.NoError(t, err)
	assert.Equal(t, 1, resp.Count)
	assert.Equal(t, "ldap.one", resp.Users[0]["username"])
}

func TestHandleUnlockLDAPUsers(t *testing.T) {
	p, api := setupTestPluginWithAPI()

	adminUser := &model.User{
		Id:    "admin-user-id",
		Roles: model.SystemAdminRoleId,
	}
	api.On("GetUser", "admin-user-id").Return(adminUser, nil)
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything).Maybe()

	api.On("UpdateUserAuth", "target-ldap-uid", &model.UserAuth{
		AuthService: "",
		AuthData:    nil,
	}).Return(&model.UserAuth{}, nil)

	body := `{"user_ids": ["target-ldap-uid"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/unlock-ldap", strings.NewReader(body))
	req.Header.Set("Mattermost-User-Id", "admin-user-id")
	w := httptest.NewRecorder()

	p.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Success  bool     `json:"success"`
		Unlocked int      `json:"unlocked"`
		Failed   []string `json:"failed"`
	}
	err := json.NewDecoder(w.Body).Decode(&resp)
	assert.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, 1, resp.Unlocked)
	assert.Empty(t, resp.Failed)
}
