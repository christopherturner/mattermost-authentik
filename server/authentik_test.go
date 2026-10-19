package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
}
