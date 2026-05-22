package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Azure SDK error fragments used by wrapAzureResourceError to classify
// runtime errors. We match on substring rather than struct-unwrap because
// azcore wraps errors in *azcore.ResponseError but the wrapper's exact
// type depends on which SDK package raised it, and the SDKs spell the
// codes consistently in the message body.
const (
	azureErrCodeResourceNotFound  = "ResourceNotFound"  // generic storage
	azureErrCodeContainerNotFound = "ContainerNotFound" // azblob containers
	azureErrCodeQueueNotFound     = "QueueNotFound"     // azqueue queues
	azureErrCodeNotFoundStatus    = "404"               // last-resort fallback

	// Authorization / authentication codes emitted by Azure when an SP
	// is missing the required data-plane RBAC role. We surface these
	// with an actionable "verify the role" hint instead of the generic
	// branch so operators don't conflate "missing resource" with
	// "missing role" (a common SP-mis-RBAC confusion).
	azureErrCodeAuthorizationFailure  = "AuthorizationFailure"
	azureErrCodeAuthorizationMismatch = "AuthorizationPermissionMismatch"
	azureErrCodeAuthenticationFailed  = "AuthenticationFailed"
	azureErrCodeForbiddenStatus       = "403"
)

// wrapAzureResourceError turns a raw SDK 404 into a sanitized actionable
// error that tells the operator to pre-provision the resource. SP-mode
// 403 / AuthorizationPermissionMismatch errors get a separate RBAC hint
// because they are the canonical "wrong RBAC role on the SP" failure
// mode and need a different remediation (grant the role, not create the
// resource). Other errors are returned with sanitization but no rewriting.
func wrapAzureResourceError(err error, resourceKind, resourceName, authMode string) error {
	if err == nil {
		return nil
	}
	clean := sanitizeAzureError(err)
	if isAzureResourceNotFound(clean) {
		switch authMode {
		case AzureAuthServicePrincipal:
			return fmt.Errorf("%s %q not found: in service-principal mode, the plugin does not auto-create resources. Pre-provision via the Azure portal or `az` CLI, then retry. (SDK: %s)",
				resourceKind, resourceName, clean)
		default:
			return fmt.Errorf("%s %q not found: %s", resourceKind, resourceName, clean)
		}
	}
	if isAzureAuthorizationFailure(clean) {
		switch authMode {
		case AzureAuthServicePrincipal:
			// Note: Azure sometimes returns 403 (instead of 404) when both the
			// RBAC role and the resource are missing, so we mention both
			// possibilities. The RBAC-role check is the typical fix.
			return fmt.Errorf("%s %q authorization failed: verify the Service Principal has the required data-plane RBAC role on this resource (Storage Queue/Blob Data Contributor or Azure Service Bus Data Sender/Receiver depending on provider), and that the resource exists. (SDK: %s)",
				resourceKind, resourceName, clean)
		default:
			return fmt.Errorf("%s %q authorization failed: verify the shared-key / SAS rule has the required permissions. (SDK: %s)",
				resourceKind, resourceName, clean)
		}
	}
	return fmt.Errorf("%s %q: %s", resourceKind, resourceName, clean)
}

// isAzureResourceNotFound returns true if the (already-sanitized) error
// string looks like a 404 from an Azure storage or messaging SDK. The
// match is case-insensitive on the error-code strings and falls back to
// substring "404" so we don't miss a sanitized-but-otherwise-untouched
// HTTP response line.
func isAzureResourceNotFound(s string) bool {
	if s == "" {
		return false
	}
	lower := strings.ToLower(s)
	for _, code := range []string{
		strings.ToLower(azureErrCodeResourceNotFound),
		strings.ToLower(azureErrCodeContainerNotFound),
		strings.ToLower(azureErrCodeQueueNotFound),
	} {
		if strings.Contains(lower, code) {
			return true
		}
	}
	// Last-resort: "404" appears in HTTP response lines that the SDK
	// preserves verbatim (e.g. "RESPONSE 404: 404 The specified ...").
	// We require it to be tied to a "not" or "exist" keyword to avoid
	// false positives on unrelated 404-bearing strings.
	if strings.Contains(lower, azureErrCodeNotFoundStatus) &&
		(strings.Contains(lower, "not found") || strings.Contains(lower, "does not exist")) {
		return true
	}
	return false
}

// isAzureAuthorizationFailure returns true if the (already-sanitized)
// error string indicates a 403 / authorization / authentication failure.
// This is the canonical "SP is missing the required RBAC role" failure
// mode that needs a different remediation than "resource doesn't exist".
func isAzureAuthorizationFailure(s string) bool {
	if s == "" {
		return false
	}
	lower := strings.ToLower(s)
	for _, code := range []string{
		strings.ToLower(azureErrCodeAuthorizationMismatch),
		strings.ToLower(azureErrCodeAuthorizationFailure),
		strings.ToLower(azureErrCodeAuthenticationFailed),
	} {
		if strings.Contains(lower, code) {
			return true
		}
	}
	if strings.Contains(lower, azureErrCodeForbiddenStatus) &&
		(strings.Contains(lower, "forbidden") || strings.Contains(lower, "unauthorized")) {
		return true
	}
	return false
}

// Azure error sanitization. Errors returned by the Azure SDK and by
// azidentity can include credential material: SAS keys and signatures
// (shared-key / connection-string mode), OAuth2 form bodies with
// `client_secret=...` (SP mode), AAD bearer tokens, and access/refresh
// tokens in JSON. Every site that logs or returns an SDK error to a
// user-facing path MUST run the string through sanitizeAzureError first.
//
// The historical sanitizeServiceBusError helper covered only SAS-shaped
// material in the Service Bus provider. Service Principal auth surfaces
// the same SDK error machinery in Queue and Blob providers (via
// azidentity token-acquisition failures), so the regex set lives here in
// shared scope and is applied by all three providers and the
// test-connection API paths.

var (
	// Shared-key / SAS patterns.
	sanitizeSharedAccessKey = regexp.MustCompile(`(?i)SharedAccessKey=[^;\s]+`)
	sanitizeSharedAccessSig = regexp.MustCompile(`(?i)SharedAccessSignature=[^;\s]+`)
	sanitizeSigQuery        = regexp.MustCompile(`(?i)([?&])sig=[^&\s]+`)
	// User-delegation SAS object/tenant identifiers (signedoid / signedtid
	// in the canonical form; skoid / sktid in the URL-encoded form). These
	// are not secrets in the SAS-signature sense but pivot to tenant
	// enumeration, and AAD object IDs are PII-adjacent. Redact for the
	// same defense-in-depth reasons as the other SAS params.
	sanitizeSASOid = regexp.MustCompile(`(?i)([?&])(skoid|sktid|signedoid|signedtid)=[^&\s]+`)

	// OAuth2 client-credentials form body. The azidentity SDK posts
	// `client_id=...&client_secret=...` to the AAD token endpoint; on
	// transport-layer failures (TLS, DNS, 5xx) the buffered request body
	// can appear inside the wrapped error.
	sanitizeClientSecret    = regexp.MustCompile(`(?i)client_secret=[^&\s"]+`)
	sanitizeAssertion       = regexp.MustCompile(`(?i)\bassertion=[^&\s"]+`)
	sanitizeClientAssertion = regexp.MustCompile(`(?i)client_assertion=[^&\s"]+`)
	sanitizePasswordForm    = regexp.MustCompile(`(?i)\bpassword=[^&\s"]+`)

	// AAD bearer tokens (B64-encoded JWT or opaque) carried in
	// Authorization headers or echoed in error context.
	sanitizeBearer = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]+`)

	// AAD token endpoint JSON responses, on error paths where the body is
	// echoed.
	sanitizeAccessTokenJSON  = regexp.MustCompile(`"access_token"\s*:\s*"[^"]*"`)
	sanitizeRefreshTokenJSON = regexp.MustCompile(`"refresh_token"\s*:\s*"[^"]*"`)
	sanitizeIDTokenJSON      = regexp.MustCompile(`"id_token"\s*:\s*"[^"]*"`)
)

// sanitizeAzureError returns a safe-to-log rendering of an Azure SDK error.
// nil input is rendered as the empty string so callers can use the result
// in format strings without guarding for nil. Apply at every boundary
// where an SDK error flows into a log line or a user-facing string:
// provider constructors, Publish/Send paths, Subscribe ack sites, Close,
// and the test-connection API handlers.
func sanitizeAzureError(err error) string {
	if err == nil {
		return ""
	}
	return sanitizeAzureString(err.Error())
}

// sanitizeAzureString applies the redaction regex set to an arbitrary
// string. Exposed for callers that already have err.Error() in hand (e.g.
// when formatting a context line ahead of the SDK error) or for testing
// the regex coverage directly.
func sanitizeAzureString(s string) string {
	s = sanitizeSharedAccessKey.ReplaceAllString(s, "SharedAccessKey=REDACTED")
	s = sanitizeSharedAccessSig.ReplaceAllString(s, "SharedAccessSignature=REDACTED")
	s = sanitizeSigQuery.ReplaceAllString(s, "${1}sig=REDACTED")
	s = sanitizeSASOid.ReplaceAllString(s, "${1}${2}=REDACTED")
	s = sanitizeClientAssertion.ReplaceAllString(s, "client_assertion=REDACTED")
	s = sanitizeClientSecret.ReplaceAllString(s, "client_secret=REDACTED")
	s = sanitizeAssertion.ReplaceAllString(s, "assertion=REDACTED")
	s = sanitizePasswordForm.ReplaceAllString(s, "password=REDACTED")
	s = sanitizeBearer.ReplaceAllString(s, "Bearer REDACTED")
	s = sanitizeAccessTokenJSON.ReplaceAllString(s, `"access_token":"REDACTED"`)
	s = sanitizeRefreshTokenJSON.ReplaceAllString(s, `"refresh_token":"REDACTED"`)
	s = sanitizeIDTokenJSON.ReplaceAllString(s, `"id_token":"REDACTED"`)
	return s
}
