package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSignAndVerifyState(t *testing.T) {
	p := &Plugin{
		encryptionKey: "test-secret-encryption-key-for-hmac",
	}

	t.Run("valid state roundtrip", func(t *testing.T) {
		token := "random-state-token-12345"
		signed := p.signState(token)

		extracted, err := p.verifyAndExtractState(signed)
		assert.NoError(t, err)
		assert.Equal(t, token, extracted)
	})

	t.Run("tampered state token is rejected", func(t *testing.T) {
		token := "random-state-token-12345"
		signed := p.signState(token)

		tampered := "tampered-token" + signed[len(token):]
		_, err := p.verifyAndExtractState(tampered)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "state signature mismatch")
	})

	t.Run("malformed state without separator is rejected", func(t *testing.T) {
		_, err := p.verifyAndExtractState("invalidtokenwithoutcolon")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "malformed state parameter")
	})

	t.Run("wrong key fails signature verification", func(t *testing.T) {
		token := "random-state-token-12345"
		signed := p.signState(token)

		pOther := &Plugin{
			encryptionKey: "different-encryption-key",
		}
		_, err := pOther.verifyAndExtractState(signed)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "state signature mismatch")
	})
}

func TestIsAllowedMobileScheme(t *testing.T) {
	testCases := []struct {
		url      string
		expected bool
	}{
		{"mmauth://callback", true},
		{"mmauthbeta://callback", true},
		{"mmauth://callback?token=123", true},
		{"mmauthbeta://callback?token=123&csrf=abc", true},
		{"mmauth://callback.evil.com", false},
		{"mmauth://other", false},
		{"https://evil.com/callback", false},
		{"javascript:alert(1)", false},
		{"", false},
	}

	for _, tc := range testCases {
		t.Run(tc.url, func(t *testing.T) {
			actual := isAllowedMobileScheme(tc.url)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestGetCookiePath(t *testing.T) {
	assert.Equal(t, "/", getCookiePath(""))
	assert.Equal(t, "/", getCookiePath("https://chat.example.com"))
	assert.Equal(t, "/", getCookiePath("https://chat.example.com/"))
	assert.Equal(t, "/mattermost", getCookiePath("https://chat.example.com/mattermost"))
	assert.Equal(t, "/mattermost", getCookiePath("https://chat.example.com/mattermost/"))
}
