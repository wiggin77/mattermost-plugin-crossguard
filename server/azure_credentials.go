package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/mattermost/mattermost/server/public/plugin"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
)

// azcoreCloudConfig aliases the SDK's cloud.Configuration so that other
// files in this package can name the cloud option type without importing
// the azcore/cloud subpackage directly. Keeps the constructor-call surface
// area narrower.
type azcoreCloudConfig = cloud.Configuration

// Azure data-plane OAuth2 scopes used by the save-time GetToken probe and
// runtime credential acquisition. These are documented at SDK level in
// azqueue/internal/base/clients.go, azblob/internal/base/clients.go, and
// azservicebus/internal/sbauth/token_provider.go (the latter intentionally
// uses the double-slash form).
const (
	azureStorageScope    = "https://storage.azure.com/.default"
	azureServiceBusScope = "https://servicebus.azure.net//.default"

	// azureCredentialProbeTimeout bounds the save-time GetToken call so a
	// hung AAD endpoint cannot block plugin config save indefinitely.
	azureCredentialProbeTimeout = 10 * time.Second

	// secretSourceMaxFileSize is the upper bound for client_secret_file
	// contents; well above any realistic AAD secret length and defends
	// against an operator pointing the plugin at a 10 GB log file.
	secretSourceMaxFileSize = 64 * 1024
)

// resolveAzureCloud maps the configured azure_cloud string to the SDK's
// cloud.Configuration. Empty string defaults to Azure Public. Unknown
// values return an error so validation can surface a clear message.
func resolveAzureCloud(name string) (cloud.Configuration, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", AzureCloudPublic:
		return cloud.AzurePublic, nil
	case AzureCloudUSGov:
		return cloud.AzureGovernment, nil
	case AzureCloudChina:
		return cloud.AzureChina, nil
	default:
		return cloud.Configuration{}, fmt.Errorf("unknown azure_cloud %q (allowed: %s, %s, %s)",
			name, AzureCloudPublic, AzureCloudUSGov, AzureCloudChina)
	}
}

// azureSecretSource captures the three mutually-exclusive ways to supply
// a Service Principal client secret. The validator enforces that exactly
// one is set in SP mode; resolveAzureSecret turns the source into a
// plaintext secret at construction time.
type azureSecretSource struct {
	Inline   string
	EnvVar   string
	FilePath string
}

// resolveAzureSecret returns the resolved secret value or an error. Mutual
// exclusion is enforced (zero or multiple sources -> error). Resolution
// happens at construction time so secret rotation in a Key Vault / CSI
// mount works without plugin config rewrites.
func resolveAzureSecret(src azureSecretSource) (string, error) {
	count := 0
	if src.Inline != "" {
		count++
	}
	if src.EnvVar != "" {
		count++
	}
	if src.FilePath != "" {
		count++
	}

	switch count {
	case 0:
		return "", errors.New("no client secret source set; provide exactly one of client_secret, client_secret_env, or client_secret_file")
	case 1:
		// fall through to resolution
	default:
		return "", errors.New("multiple client secret sources set; provide exactly one of client_secret, client_secret_env, or client_secret_file")
	}

	switch {
	case src.Inline != "":
		return src.Inline, nil
	case src.EnvVar != "":
		v, ok := os.LookupEnv(src.EnvVar)
		if !ok {
			return "", fmt.Errorf("client_secret_env %q is not set in the environment", src.EnvVar)
		}
		if v == "" {
			return "", fmt.Errorf("client_secret_env %q is set but empty", src.EnvVar)
		}
		return v, nil
	case src.FilePath != "":
		info, err := os.Stat(src.FilePath)
		if err != nil {
			return "", fmt.Errorf("client_secret_file: %w", err)
		}
		if info.Size() > secretSourceMaxFileSize {
			return "", fmt.Errorf("client_secret_file: file is larger than %d bytes (got %d)", secretSourceMaxFileSize, info.Size())
		}
		raw, err := os.ReadFile(src.FilePath)
		if err != nil {
			return "", fmt.Errorf("client_secret_file: %w", err)
		}
		// Strip a single trailing newline (common with `echo "secret" > file`).
		// Do NOT trim all whitespace: leading/trailing whitespace inside the
		// actual secret would be a config bug, not something the plugin
		// should silently fix.
		v := strings.TrimRight(string(raw), "\n\r")
		if v == "" {
			return "", fmt.Errorf("client_secret_file %q is empty", src.FilePath)
		}
		return v, nil
	}
	// Unreachable: count == 1 already gated all three.
	return "", errors.New("internal: unreachable secret source")
}

// azureServicePrincipalParams holds the inputs needed to construct a
// ClientSecretCredential. The resolved (plaintext) secret is passed in
// directly so the caller decides whether to log/audit before calling.
type azureServicePrincipalParams struct {
	TenantID string
	ClientID string
	Secret   string
	Cloud    cloud.Configuration
}

// buildClientSecretCredential constructs an azidentity ClientSecretCredential
// with the configured cloud. The returned azcore.TokenCredential is the
// interface accepted by all three Azure SDK clients (azqueue, azblob/container,
// azservicebus).
func buildClientSecretCredential(p azureServicePrincipalParams) (azcore.TokenCredential, error) {
	opts := &azidentity.ClientSecretCredentialOptions{
		ClientOptions: azcore.ClientOptions{Cloud: p.Cloud},
	}
	cred, err := azidentity.NewClientSecretCredential(p.TenantID, p.ClientID, p.Secret, opts)
	if err != nil {
		return nil, fmt.Errorf("azidentity NewClientSecretCredential: %s", sanitizeAzureError(err))
	}
	return cred, nil
}

// probeAzureCredential calls GetToken against the supplied data-plane scope
// with a bounded timeout. Errors are routed through sanitizeAzureError so
// AAD response bodies and request bodies (which may contain client_secret=...)
// never appear in the returned message.
func probeAzureCredential(ctx context.Context, cred azcore.TokenCredential, scope string) error {
	probeCtx, cancel := context.WithTimeout(ctx, azureCredentialProbeTimeout)
	defer cancel()
	_, err := cred.GetToken(probeCtx, policy.TokenRequestOptions{Scopes: []string{scope}})
	if err != nil {
		return fmt.Errorf("azure credential probe (scope=%s): %s", scope, sanitizeAzureError(err))
	}
	return nil
}

// probeAzureConnectionSP performs a save-time GetToken probe for the
// Service Principal in a connection config. Returns nil if the connection
// is not in SP mode (nothing to probe) or if the probe succeeds. Errors
// are sanitized before being returned to the caller.
//
// This is called from OnConfigurationChange AFTER syntactic validate() and
// BEFORE setConfiguration, so a failed probe blocks the config save.
func probeAzureConnectionSP(ctx context.Context, conn ConnectionConfig) error {
	switch conn.Provider {
	case ProviderAzureQueue:
		if conn.AzureQueue == nil || normalizeAzureAuthMode(conn.AzureQueue.AuthMode) != AzureAuthServicePrincipal {
			return nil
		}
		return probeAzureSP(ctx,
			conn.AzureQueue.TenantID, conn.AzureQueue.ClientID,
			conn.AzureQueue.ClientSecret, conn.AzureQueue.ClientSecretEnv, conn.AzureQueue.ClientSecretFile,
			conn.AzureQueue.AzureCloud, azureStorageScope, conn.Name)
	case ProviderAzureBlob:
		if conn.AzureBlob == nil || normalizeAzureAuthMode(conn.AzureBlob.AuthMode) != AzureAuthServicePrincipal {
			return nil
		}
		return probeAzureSP(ctx,
			conn.AzureBlob.TenantID, conn.AzureBlob.ClientID,
			conn.AzureBlob.ClientSecret, conn.AzureBlob.ClientSecretEnv, conn.AzureBlob.ClientSecretFile,
			conn.AzureBlob.AzureCloud, azureStorageScope, conn.Name)
	case ProviderAzureServiceBus:
		if conn.AzureServiceBus == nil || normalizeAzureAuthMode(conn.AzureServiceBus.AuthMode) != AzureAuthServicePrincipal {
			return nil
		}
		return probeAzureSP(ctx,
			conn.AzureServiceBus.TenantID, conn.AzureServiceBus.ClientID,
			conn.AzureServiceBus.ClientSecret, conn.AzureServiceBus.ClientSecretEnv, conn.AzureServiceBus.ClientSecretFile,
			conn.AzureServiceBus.AzureCloud, azureServiceBusScope, conn.Name)
	}
	return nil
}

// probeAzureSP resolves the secret, builds the credential, and runs the
// GetToken probe. Used by probeAzureConnectionSP; broken out so the four
// per-provider branches share the same code path.
func probeAzureSP(ctx context.Context, tenantID, clientID, secret, secretEnv, secretFile, cloudName, scope, connName string) error {
	resolved, err := resolveAzureSecret(azureSecretSource{Inline: secret, EnvVar: secretEnv, FilePath: secretFile})
	if err != nil {
		return fmt.Errorf("connection %q: %w", connName, err)
	}
	azCloud, err := resolveAzureCloud(cloudName)
	if err != nil {
		return fmt.Errorf("connection %q: %w", connName, err)
	}
	cred, err := buildClientSecretCredential(azureServicePrincipalParams{
		TenantID: tenantID, ClientID: clientID, Secret: resolved, Cloud: azCloud,
	})
	if err != nil {
		return fmt.Errorf("connection %q: %w", connName, err)
	}
	if err := probeAzureCredential(ctx, cred, scope); err != nil {
		return fmt.Errorf("connection %q: %w", connName, err)
	}
	return nil
}

// logAzureAuthAudit emits a successful-SP-construction audit log line so
// operators have a record of which AAD identity authenticated each
// connection. The secret value is NEVER logged; only the first 12 hex
// characters of its SHA-256 hash, which is enough to identify "which
// secret was active during incident X" without exposing material that
// could be used to reconstruct the value.
func logAzureAuthAudit(api plugin.API, provider, tenantID, clientID, azureCloud, secret string) {
	if api == nil {
		return
	}
	api.LogInfo("Azure auth (service-principal): credential constructed",
		"error_code", errcode.AzureSPAuditConstructed,
		"provider", provider,
		"auth_mode", AzureAuthServicePrincipal,
		"tenant_id", tenantID,
		"client_id", clientID,
		"azure_cloud", normalizeAzureCloudForLog(azureCloud),
		"secret_hash_prefix", secretHashPrefix(secret))
}

// secretHashPrefix returns the first 12 hex characters of SHA-256(secret).
// 48 bits is enough to identify which secret was in use, but far short of
// useful brute-force surface even with the character set known.
func secretHashPrefix(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])[:12]
}

// normalizeAzureCloudForLog renders the cloud value for audit logs: empty
// maps to "public" (the implicit default) so the log line is unambiguous.
func normalizeAzureCloudForLog(s string) string {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" {
		return AzureCloudPublic
	}
	return t
}
