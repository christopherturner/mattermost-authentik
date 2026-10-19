package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
)

var (
	validUsernameCharsRegex = regexp.MustCompile(`[^a-z0-9._-]`)
	startsLetterRegex       = regexp.MustCompile(`^[a-z]`)
)

// AuthentikUserInfo holds claims extracted from Authentik tokens and userinfo.
type AuthentikUserInfo struct {
	Subject       string   `json:"sub"`
	Email         string   `json:"email"`
	Username      string   `json:"username"`
	FirstName     string   `json:"first_name"`
	LastName      string   `json:"last_name"`
	Position      string   `json:"position,omitempty"`
	EmailVerified *bool    `json:"email_verified,omitempty"`
	Groups        []string `json:"groups,omitempty"`
}

// sanitizeUsername normalizes a username to meet Mattermost's constraints:
// - lowercase alphanumeric, dots, underscores, dashes
// - starts with a letter
// - length between 3 and 22 characters (well within Mattermost's max limit of 64)
func sanitizeUsername(input string) string {
	u := strings.ToLower(strings.TrimSpace(input))

	// Replace spaces or invalid characters with dots
	u = validUsernameCharsRegex.ReplaceAllString(u, ".")

	// Remove leading/trailing dots and repeated dots
	u = strings.Trim(u, ".-_")
	for strings.Contains(u, "..") {
		u = strings.ReplaceAll(u, "..", ".")
	}

	// Must begin with a letter
	if !startsLetterRegex.MatchString(u) {
		u = "u." + u
	}

	// Length constraints
	if len(u) > 22 {
		u = u[:22]
	}
	u = strings.TrimRight(u, ".-_")
	if len(u) < 3 {
		u = u + "user"
	}

	return u
}

// parseStringSlice parses an interface{} claim value that could be []interface{} or []string or comma-separated string.
func parseStringSlice(val interface{}) []string {
	if val == nil {
		return nil
	}

	switch v := val.(type) {
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				result = append(result, strings.TrimSpace(s))
			}
		}
		return result
	case []string:
		return v
	case string:
		parts := strings.Split(v, ",")
		result := make([]string, 0, len(parts))
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

// hasGroup checks if the slice of groups contains targetGroup or any group in a comma/semicolon/newline-separated list (case-insensitive).
func hasGroup(groups []string, targetGroupConfig string) bool {
	if strings.TrimSpace(targetGroupConfig) == "" {
		return false
	}

	targets := strings.FieldsFunc(targetGroupConfig, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})

	for _, t := range targets {
		target := strings.ToLower(strings.TrimSpace(t))
		if target == "" {
			continue
		}
		for _, g := range groups {
			if strings.ToLower(strings.TrimSpace(g)) == target {
				return true
			}
		}
	}
	return false
}

// syncAdminRole updates the user's System Admin role based on Authentik group membership.
func (p *Plugin) syncAdminRole(user *model.User, userInfo *AuthentikUserInfo, config *Configuration) error {
	if !config.SyncAdminRole {
		return nil
	}

	isInAdminGroup := false
	if config.GrantAuthentikAdmins && hasGroup(userInfo.Groups, "authentik Admins") {
		isInAdminGroup = true
	}
	if !isInAdminGroup && config.AdminGroup != "" && hasGroup(userInfo.Groups, config.AdminGroup) {
		isInAdminGroup = true
	}

	isCurrentlyAdmin := user.IsInRole(model.SystemAdminRoleId)

	if isInAdminGroup && !isCurrentlyAdmin {
		p.API.LogInfo("Promoting user to System Admin based on Authentik group",
			"user_id", user.Id, "username", user.Username)
		newRoles := strings.TrimSpace(user.Roles + " " + model.SystemAdminRoleId)
		if _, appErr := p.API.UpdateUserRoles(user.Id, newRoles); appErr != nil {
			return fmt.Errorf("failed to grant system admin role: %s", appErr.Error())
		}
		user.Roles = newRoles
	} else if !isInAdminGroup && isCurrentlyAdmin {
		p.API.LogInfo("Demoting user from System Admin based on Authentik group",
			"user_id", user.Id, "username", user.Username)

		// Remove system_admin from roles, ensuring system_user remains
		roleList := strings.Fields(user.Roles)
		var updatedList []string
		for _, r := range roleList {
			if r != model.SystemAdminRoleId {
				updatedList = append(updatedList, r)
			}
		}
		if len(updatedList) == 0 {
			updatedList = append(updatedList, model.SystemUserRoleId)
		}

		newRoles := strings.Join(updatedList, " ")
		if _, appErr := p.API.UpdateUserRoles(user.Id, newRoles); appErr != nil {
			return fmt.Errorf("failed to revoke system admin role: %s", appErr.Error())
		}
		user.Roles = newRoles
	}

	return nil
}

// ensureDefaultTeamMember ensures the user is added to the specified default team.
func (p *Plugin) ensureDefaultTeamMember(userID string, teamHandle string) error {
	if teamHandle == "" {
		return nil
	}

	team, appErr := p.API.GetTeamByName(teamHandle)
	if appErr != nil || team == nil {
		return fmt.Errorf("default team '%s' not found: %v", teamHandle, appErr)
	}

	// Check if already a member
	member, _ := p.API.GetTeamMember(team.Id, userID)
	if member != nil && member.DeleteAt == 0 {
		return nil // already member
	}

	_, createErr := p.API.CreateTeamMember(team.Id, userID)
	if createErr != nil {
		return fmt.Errorf("failed to add user to team '%s': %s", teamHandle, createErr.Error())
	}

	p.API.LogInfo("Added user to default team", "user_id", userID, "team", teamHandle)
	return nil
}
