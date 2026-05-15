package main

import (
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeAzureError_NilReturnsEmpty(t *testing.T) {
	assert.Equal(t, "", sanitizeAzureError(nil))
}

func TestSanitizeAzureError_SharedAccessKey(t *testing.T) {
	err := errors.New("auth failed: Endpoint=sb://x;SharedAccessKeyName=root;SharedAccessKey=hunter2-base64==;EntityPath=q")
	got := sanitizeAzureError(err)
	assert.Contains(t, got, "SharedAccessKey=REDACTED")
	assert.NotContains(t, got, "hunter2-base64==")
}

func TestSanitizeAzureError_SharedAccessSignature(t *testing.T) {
	err := errors.New("auth failed: SharedAccessSignature=sr=foo&sig=abc123def&se=1234")
	got := sanitizeAzureError(err)
	assert.Contains(t, got, "SharedAccessSignature=REDACTED")
	assert.NotContains(t, got, "abc123def")
}

func TestSanitizeAzureError_SigQuery(t *testing.T) {
	err := errors.New("GET https://acct.queue.core.windows.net/q?sig=long-sas-signature-value&st=1 500")
	got := sanitizeAzureError(err)
	assert.Contains(t, got, "sig=REDACTED")
	assert.NotContains(t, got, "long-sas-signature-value")
}

func TestSanitizeAzureError_ClientSecretFormBody(t *testing.T) {
	// Simulate an azidentity transport-layer failure where the wrapped error
	// includes the buffered OAuth2 token-request body.
	body := "client_id=app-uuid&client_secret=very-secret-value&grant_type=client_credentials"
	wrapped := &url.Error{
		Op:  "Post",
		URL: "https://login.microsoftonline.com/tenant/oauth2/v2.0/token",
		Err: errors.New("tls: handshake failure; request body: " + body),
	}
	got := sanitizeAzureError(wrapped)
	assert.Contains(t, got, "client_secret=REDACTED")
	assert.NotContains(t, got, "very-secret-value")
	assert.Contains(t, got, "client_id=app-uuid", "client_id should NOT be redacted (it is operationally safe)")
}

func TestSanitizeAzureError_AssertionFormBody(t *testing.T) {
	err := errors.New("token request failed: client_assertion_type=jwt&assertion=eyJhbGciOi.JqwT.body.sig&grant_type=client_credentials")
	got := sanitizeAzureError(err)
	assert.Contains(t, got, "assertion=REDACTED")
	assert.NotContains(t, got, "eyJhbGciOi.JqwT.body.sig")
}

func TestSanitizeAzureError_BearerToken(t *testing.T) {
	err := errors.New("response 401: Authorization: Bearer eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiJ9.abc.def")
	got := sanitizeAzureError(err)
	assert.Contains(t, got, "Bearer REDACTED")
	assert.NotContains(t, got, "eyJ0eXAi")
}

func TestSanitizeAzureError_AccessTokenJSON(t *testing.T) {
	err := errors.New(`AAD response: {"access_token":"eyJ.real.token","expires_in":3600,"token_type":"Bearer"}`)
	got := sanitizeAzureError(err)
	assert.Contains(t, got, `"access_token":"REDACTED"`)
	assert.NotContains(t, got, "eyJ.real.token")
	assert.Contains(t, got, `"expires_in":3600`, "non-sensitive JSON fields preserved")
}

func TestSanitizeAzureError_RefreshTokenJSON(t *testing.T) {
	err := errors.New(`{"refresh_token":"0.refresh-token-material","token_type":"Bearer"}`)
	got := sanitizeAzureError(err)
	assert.Contains(t, got, `"refresh_token":"REDACTED"`)
	assert.NotContains(t, got, "0.refresh-token-material")
}

func TestSanitizeAzureError_MultiplePatternsInOneString(t *testing.T) {
	err := errors.New("error: SharedAccessKey=k1; client_secret=s1; Bearer abctoken123")
	got := sanitizeAzureError(err)
	assert.NotContains(t, got, "k1")
	assert.NotContains(t, got, "s1")
	assert.NotContains(t, got, "abctoken123")
	assert.Contains(t, got, "SharedAccessKey=REDACTED")
	assert.Contains(t, got, "client_secret=REDACTED")
	assert.Contains(t, got, "Bearer REDACTED")
}

func TestSanitizeAzureError_ClientSecretCaseInsensitive(t *testing.T) {
	// AAD form posts are lowercase, but log echoes may upcase.
	for _, variant := range []string{"client_secret=topsec", "Client_Secret=topsec", "CLIENT_SECRET=topsec"} {
		err := errors.New("body=" + variant)
		got := sanitizeAzureError(err)
		assert.NotContains(t, got, "topsec", "variant %q must be scrubbed", variant)
	}
}

func TestSanitizeAzureString_NoSensitiveContent_ReturnsUnchanged(t *testing.T) {
	in := "container not found: q=alpha&tenant=abc.onmicrosoft.com"
	got := sanitizeAzureString(in)
	// Tenant identifiers are intentionally NOT redacted (they're operationally
	// safe per the plan's decision matrix); confirm nothing got over-scrubbed.
	assert.Equal(t, in, got)
}

func TestSanitizeAzureError_ServiceBusErrorWrapper(t *testing.T) {
	// The thin sanitizeServiceBusError wrapper must delegate to the shared helper.
	err := errors.New("SB error: SharedAccessKey=leaky")
	got := sanitizeServiceBusError(err)
	assert.Contains(t, got, "SharedAccessKey=REDACTED")
	assert.False(t, strings.Contains(got, "leaky"))
}
