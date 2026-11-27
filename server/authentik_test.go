package main

import (
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestSanitizeUsername(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"john.doe", "john.doe"},
		{"John Doe", "john.doe"},
		{"123user", "u.123user"},
		{"-special-user-", "special-user"},
		{"user@company.com", "user.company.com"},
		{"a", "auser"},
		{"ab", "abuser"},
		{"verylongusernamethatexceedstwentyandcharacters", "verylongusernamethatex"},
		{"alexander.christopher.dot", "alexander.christopher"},
		{"twenty.two.characters.here", "twenty.two.characters"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			actual := sanitizeUsername(tc.input)
			assert.Equal(t, tc.expected, actual)
			assert.True(t, len(actual) >= 3, "Username must be at least 3 chars")
			assert.True(t, len(actual) <= 22, "Username must be at most 22 chars")
			assert.Regexp(t, `^[a-z]`, actual, "Username must begin with a letter")
		})
	}
}

func TestParseStringSlice(t *testing.T) {
	t.Run("from interface slice", func(t *testing.T) {
		input := []interface{}{"group1", "group2", "  group3  "}
		expected := []string{"group1", "group2", "group3"}
		assert.Equal(t, expected, parseStringSlice(input))
	})

	t.Run("from string slice", func(t *testing.T) {
		input := []string{"group1", "group2"}
		assert.Equal(t, input, parseStringSlice(input))
	})

	t.Run("from comma-separated string", func(t *testing.T) {
		input := "group1, group2, group3"
		expected := []string{"group1", "group2", "group3"}
		assert.Equal(t, expected, parseStringSlice(input))
	})

	t.Run("from nil", func(t *testing.T) {
		assert.Nil(t, parseStringSlice(nil))
	})
}

func TestHasGroup(t *testing.T) {
	groups := []string{"authentik Admins", "Developers", "DevOps"}

	assert.True(t, hasGroup(groups, "authentik Admins"))
	assert.True(t, hasGroup(groups, "authentik admins"))
	assert.True(t, hasGroup(groups, "DEVELOPERS"))
	assert.False(t, hasGroup(groups, "Sales"))
	assert.False(t, hasGroup(groups, ""))

	// Multiple comma-separated groups
	assert.True(t, hasGroup(groups, "mattermost-admins, authentik Admins"))
	assert.True(t, hasGroup(groups, "mattermost-admins, developers, it-ops"))
	assert.True(t, hasGroup(groups, "Sales; Support; DevOps"))
	assert.False(t, hasGroup(groups, "Sales, Support, Marketing"))
}

func TestSyncAdminRole(t *testing.T) {
	t.Run("promotes user to system_admin when in GrantAuthentikAdmins group", func(t *testing.T) {
		p, api := setupTestPluginWithAPI()
		api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

		user := &model.User{Id: "u1", Username: "alice", Roles: model.SystemUserRoleId}
		expectedRoles := model.SystemUserRoleId + " " + model.SystemAdminRoleId
		api.On("UpdateUserRoles", "u1", expectedRoles).Return(&model.User{Id: "u1", Roles: expectedRoles}, nil)

		err := p.syncAdminRole(user, &AuthentikUserInfo{Groups: []string{"authentik Admins"}}, &Configuration{
			SyncAdminRole:        true,
			GrantAuthentikAdmins: true,
		})
		require.NoError(t, err)
		assert.Equal(t, expectedRoles, user.Roles)
	})

	t.Run("demotes user while preserving system_user when removed from admin groups", func(t *testing.T) {
		p, api := setupTestPluginWithAPI()
		api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()

		user := &model.User{Id: "u1", Username: "alice", Roles: model.SystemUserRoleId + " " + model.SystemAdminRoleId}
		api.On("UpdateUserRoles", "u1", model.SystemUserRoleId).Return(&model.User{Id: "u1", Roles: model.SystemUserRoleId}, nil)

		err := p.syncAdminRole(user, &AuthentikUserInfo{Groups: []string{"engineering"}}, &Configuration{
			SyncAdminRole:        true,
			GrantAuthentikAdmins: true,
			AdminGroup:           "mattermost-admins",
		})
		require.NoError(t, err)
		assert.Equal(t, model.SystemUserRoleId, user.Roles)
	})
}

func TestEnsureDefaultTeamMember(t *testing.T) {
	t.Run("skips CreateTeamMember when already an active member", func(t *testing.T) {
		p, api := setupTestPluginWithAPI()
		api.On("GetTeamByName", "main").Return(&model.Team{Id: "team-1", Name: "main"}, nil)
		api.On("GetTeamMember", "team-1", "u1").Return(&model.TeamMember{TeamId: "team-1", UserId: "u1", DeleteAt: 0}, nil)

		err := p.ensureDefaultTeamMember("u1", "main")
		require.NoError(t, err)
		api.AssertNotCalled(t, "CreateTeamMember", mock.Anything, mock.Anything)
	})

	t.Run("adds user to default team when not yet a member", func(t *testing.T) {
		p, api := setupTestPluginWithAPI()
		api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
		api.On("GetTeamByName", "main").Return(&model.Team{Id: "team-1", Name: "main"}, nil)
		api.On("GetTeamMember", "team-1", "u1").Return(nil, model.NewAppError("GetTeamMember", "not_found", nil, "", 404))
		api.On("CreateTeamMember", "team-1", "u1").Return(&model.TeamMember{TeamId: "team-1", UserId: "u1"}, nil)

		err := p.ensureDefaultTeamMember("u1", "main")
		require.NoError(t, err)
		api.AssertCalled(t, "CreateTeamMember", "team-1", "u1")
	})
}
