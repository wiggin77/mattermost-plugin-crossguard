package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// stubTokenCredential is a test double for azcore.TokenCredential. Used to
// exercise probeAzureCredential and the SP-construction paths without an AAD
// round trip. When err is nil, GetToken returns a synthetic token; otherwise
// it returns err so tests can verify sanitization.
type stubTokenCredential struct {
	err   error
	block <-chan struct{} // optional: block GetToken until channel closes
}

func (s stubTokenCredential) GetToken(ctx context.Context, _ policy.TokenRequestOptions) (azcore.AccessToken, error) {
	if s.block != nil {
		select {
		case <-s.block:
		case <-ctx.Done():
			return azcore.AccessToken{}, ctx.Err()
		}
	}
	if s.err != nil {
		return azcore.AccessToken{}, s.err
	}
	return azcore.AccessToken{Token: "stub-token", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

// ----- resolveAzureSecret -----

func TestResolveAzureSecret_Inline(t *testing.T) {
	got, err := resolveAzureSecret(azureSecretSource{Inline: "topsec"})
	require.NoError(t, err)
	assert.Equal(t, "topsec", got)
}

func TestResolveAzureSecret_EnvVar(t *testing.T) {
	t.Setenv("CG_TEST_SECRET", "from-env")
	got, err := resolveAzureSecret(azureSecretSource{EnvVar: "CG_TEST_SECRET"})
	require.NoError(t, err)
	assert.Equal(t, "from-env", got)
}

func TestResolveAzureSecret_EnvVarMissing(t *testing.T) {
	require.NoError(t, os.Unsetenv("CG_TEST_SECRET_MISSING"))
	_, err := resolveAzureSecret(azureSecretSource{EnvVar: "CG_TEST_SECRET_MISSING"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not set")
}

func TestResolveAzureSecret_EnvVarEmpty(t *testing.T) {
	t.Setenv("CG_TEST_SECRET_EMPTY", "")
	_, err := resolveAzureSecret(azureSecretSource{EnvVar: "CG_TEST_SECRET_EMPTY"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestResolveAzureSecret_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	require.NoError(t, os.WriteFile(path, []byte("from-file\n"), 0o600))
	got, err := resolveAzureSecret(azureSecretSource{FilePath: path})
	require.NoError(t, err)
	assert.Equal(t, "from-file", got, "trailing newline stripped")
}

func TestResolveAzureSecret_FileMissing(t *testing.T) {
	_, err := resolveAzureSecret(azureSecretSource{FilePath: "/nonexistent/secret"})
	require.Error(t, err)
}

func TestResolveAzureSecret_FileTooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge")
	require.NoError(t, os.WriteFile(path, make([]byte, secretSourceMaxFileSize+1), 0o600))
	_, err := resolveAzureSecret(azureSecretSource{FilePath: path})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "larger than")
}

func TestResolveAzureSecret_NoSourcesIsError(t *testing.T) {
	_, err := resolveAzureSecret(azureSecretSource{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no client secret source")
}

func TestResolveAzureSecret_MultipleSourcesIsError(t *testing.T) {
	_, err := resolveAzureSecret(azureSecretSource{Inline: "a", EnvVar: "X"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "multiple")
}

// ----- resolveAzureCloud -----

func TestResolveAzureCloud(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"", false},
		{"public", false},
		{"PUBLIC", false},
		{"usgov", false},
		{"china", false},
		{"sovereign", true},
		{"PublicX", true},
	}
	for _, tc := range cases {
		_, err := resolveAzureCloud(tc.in)
		if tc.wantErr {
			assert.Error(t, err, "input %q", tc.in)
		} else {
			assert.NoError(t, err, "input %q", tc.in)
		}
	}
}

// ----- validation: per-mode exclusive logic -----

func TestValidateAzureQueueConnection_SPMode_Valid(t *testing.T) {
	conn := ConnectionConfig{
		Name:     "q",
		Provider: ProviderAzureQueue,
		AzureQueue: &AzureQueueProviderConfig{
			QueueServiceURL: "https://a.queue.core.windows.net",
			QueueName:       "myqueue",
			AuthMode:        AzureAuthServicePrincipal,
			TenantID:        "11111111-2222-3333-4444-555555555555",
			ClientID:        "client-uuid",
			ClientSecret:    "topsec",
		},
	}
	errs := validateAzureQueueConnection(conn, "test")
	assert.Empty(t, errs)
}

func TestValidateAzureQueueConnection_SPMode_RejectsAccountKey(t *testing.T) {
	conn := ConnectionConfig{
		Name:     "q",
		Provider: ProviderAzureQueue,
		AzureQueue: &AzureQueueProviderConfig{
			QueueServiceURL: "https://a.queue.core.windows.net",
			QueueName:       "myqueue",
			AuthMode:        AzureAuthServicePrincipal,
			TenantID:        "11111111-2222-3333-4444-555555555555",
			ClientID:        "client-uuid",
			ClientSecret:    "topsec",
			AccountKey:      "leftover-key", // must be empty in SP mode
		},
	}
	errs := validateAzureQueueConnection(conn, "test")
	require.NotEmpty(t, errs)
	assert.Contains(t, strings.Join(errs, " "), "account_key must be empty")
}

func TestValidateAzureQueueConnection_SharedKeyMode_RejectsSPFields(t *testing.T) {
	conn := ConnectionConfig{
		Name:     "q",
		Provider: ProviderAzureQueue,
		AzureQueue: &AzureQueueProviderConfig{
			QueueServiceURL: "https://a.queue.core.windows.net",
			QueueName:       "myqueue",
			AccountName:     "acct",
			AccountKey:      "abc==",
			TenantID:        "leftover-from-prior-sp-config", // must be empty
		},
	}
	errs := validateAzureQueueConnection(conn, "test")
	require.NotEmpty(t, errs)
	assert.Contains(t, strings.Join(errs, " "), "tenant_id must be empty")
}

func TestValidateAzureQueueConnection_UnknownAuthMode(t *testing.T) {
	conn := ConnectionConfig{
		Name:     "q",
		Provider: ProviderAzureQueue,
		AzureQueue: &AzureQueueProviderConfig{
			QueueServiceURL: "https://a.queue.core.windows.net",
			QueueName:       "myqueue",
			AuthMode:        "service_principal", // snake_case typo
		},
	}
	errs := validateAzureQueueConnection(conn, "test")
	require.NotEmpty(t, errs)
	assert.Contains(t, strings.Join(errs, " "), "auth_mode must be")
}

func TestValidateAzureQueueConnection_SPMode_MissingFields(t *testing.T) {
	conn := ConnectionConfig{
		Name:     "q",
		Provider: ProviderAzureQueue,
		AzureQueue: &AzureQueueProviderConfig{
			QueueServiceURL: "https://a.queue.core.windows.net",
			QueueName:       "myqueue",
			AuthMode:        AzureAuthServicePrincipal,
			// no tenant/client/secret
		},
	}
	errs := validateAzureQueueConnection(conn, "test")
	require.NotEmpty(t, errs)
	joined := strings.Join(errs, " ")
	assert.Contains(t, joined, "tenant_id is required")
	assert.Contains(t, joined, "client_id is required")
	assert.Contains(t, joined, "client_secret")
}

func TestValidateAzureQueueConnection_TenantID_FQDNAccepted(t *testing.T) {
	conn := ConnectionConfig{
		Name:     "q",
		Provider: ProviderAzureQueue,
		AzureQueue: &AzureQueueProviderConfig{
			QueueServiceURL: "https://a.queue.core.windows.net",
			QueueName:       "myqueue",
			AuthMode:        AzureAuthServicePrincipal,
			TenantID:        "contoso.onmicrosoft.com",
			ClientID:        "c",
			ClientSecret:    "s",
		},
	}
	errs := validateAzureQueueConnection(conn, "test")
	assert.Empty(t, errs, "FQDN tenant_id should be accepted")
}

func TestValidateAzureQueueConnection_MultipleSecretSourcesIsError(t *testing.T) {
	conn := ConnectionConfig{
		Name:     "q",
		Provider: ProviderAzureQueue,
		AzureQueue: &AzureQueueProviderConfig{
			QueueServiceURL: "https://a.queue.core.windows.net",
			QueueName:       "myqueue",
			AuthMode:        AzureAuthServicePrincipal,
			TenantID:        "11111111-2222-3333-4444-555555555555",
			ClientID:        "c",
			ClientSecret:    "inline",
			ClientSecretEnv: "X",
		},
	}
	errs := validateAzureQueueConnection(conn, "test")
	require.NotEmpty(t, errs)
	assert.Contains(t, strings.Join(errs, " "), "exactly one")
}

// ----- validateAzureBlobConnection matrix (mirrors Queue/SB) -----

func TestValidateAzureBlobConnection_SPMode_Valid(t *testing.T) {
	conn := ConnectionConfig{
		Name:     "b",
		Provider: ProviderAzureBlob,
		AzureBlob: &AzureBlobProviderConfig{
			ServiceURL:        "https://a.blob.core.windows.net",
			BlobContainerName: "container",
			AuthMode:          AzureAuthServicePrincipal,
			TenantID:          "11111111-2222-3333-4444-555555555555",
			ClientID:          "client-uuid",
			ClientSecret:      "topsec",
		},
	}
	errs := validateAzureBlobConnection(conn, "test")
	assert.Empty(t, errs)
}

func TestValidateAzureBlobConnection_SPMode_RejectsAccountKey(t *testing.T) {
	conn := ConnectionConfig{
		Name:     "b",
		Provider: ProviderAzureBlob,
		AzureBlob: &AzureBlobProviderConfig{
			ServiceURL:        "https://a.blob.core.windows.net",
			BlobContainerName: "container",
			AuthMode:          AzureAuthServicePrincipal,
			TenantID:          "11111111-2222-3333-4444-555555555555",
			ClientID:          "client-uuid",
			ClientSecret:      "topsec",
			AccountKey:        "leftover-key", // must be empty in SP mode
		},
	}
	errs := validateAzureBlobConnection(conn, "test")
	require.NotEmpty(t, errs)
	assert.Contains(t, strings.Join(errs, " "), "account_key must be empty")
}

func TestValidateAzureBlobConnection_SharedKeyMode_RejectsSPFields(t *testing.T) {
	conn := ConnectionConfig{
		Name:     "b",
		Provider: ProviderAzureBlob,
		AzureBlob: &AzureBlobProviderConfig{
			ServiceURL:        "https://a.blob.core.windows.net",
			BlobContainerName: "container",
			AccountName:       "acct",
			AccountKey:        "abc==",
			TenantID:          "leftover-from-prior-sp-config",
		},
	}
	errs := validateAzureBlobConnection(conn, "test")
	require.NotEmpty(t, errs)
	assert.Contains(t, strings.Join(errs, " "), "tenant_id must be empty")
}

func TestValidateAzureBlobConnection_UnknownAuthMode(t *testing.T) {
	conn := ConnectionConfig{
		Name:     "b",
		Provider: ProviderAzureBlob,
		AzureBlob: &AzureBlobProviderConfig{
			ServiceURL:        "https://a.blob.core.windows.net",
			BlobContainerName: "container",
			AuthMode:          "service_principal", // snake_case typo
		},
	}
	errs := validateAzureBlobConnection(conn, "test")
	require.NotEmpty(t, errs)
	assert.Contains(t, strings.Join(errs, " "), "auth_mode must be")
}

func TestValidateAzureBlobConnection_SPMode_MissingFields(t *testing.T) {
	conn := ConnectionConfig{
		Name:     "b",
		Provider: ProviderAzureBlob,
		AzureBlob: &AzureBlobProviderConfig{
			ServiceURL:        "https://a.blob.core.windows.net",
			BlobContainerName: "container",
			AuthMode:          AzureAuthServicePrincipal,
		},
	}
	errs := validateAzureBlobConnection(conn, "test")
	require.NotEmpty(t, errs)
	joined := strings.Join(errs, " ")
	assert.Contains(t, joined, "tenant_id is required")
	assert.Contains(t, joined, "client_id is required")
	assert.Contains(t, joined, "client_secret")
}

func TestValidateAzureBlobConnection_MultipleSecretSourcesIsError(t *testing.T) {
	conn := ConnectionConfig{
		Name:     "b",
		Provider: ProviderAzureBlob,
		AzureBlob: &AzureBlobProviderConfig{
			ServiceURL:        "https://a.blob.core.windows.net",
			BlobContainerName: "container",
			AuthMode:          AzureAuthServicePrincipal,
			TenantID:          "11111111-2222-3333-4444-555555555555",
			ClientID:          "c",
			ClientSecret:      "inline",
			ClientSecretEnv:   "X",
		},
	}
	errs := validateAzureBlobConnection(conn, "test")
	require.NotEmpty(t, errs)
	assert.Contains(t, strings.Join(errs, " "), "exactly one")
}

func TestValidateAzureServiceBusConnection_SPMode_Valid(t *testing.T) {
	conn := ConnectionConfig{
		Name:     "sb",
		Provider: ProviderAzureServiceBus,
		AzureServiceBus: &AzureServiceBusProviderConfig{
			QueueName:           "myqueue",
			AuthMode:            AzureAuthServicePrincipal,
			ServiceBusNamespace: "myns.servicebus.windows.net",
			TenantID:            "11111111-2222-3333-4444-555555555555",
			ClientID:            "c",
			ClientSecret:        "s",
		},
	}
	errs := validateAzureServiceBusConnection(conn, "test")
	assert.Empty(t, errs)
}

func TestValidateAzureServiceBusConnection_SPMode_RejectsConnectionString(t *testing.T) {
	conn := ConnectionConfig{
		Name:     "sb",
		Provider: ProviderAzureServiceBus,
		AzureServiceBus: &AzureServiceBusProviderConfig{
			QueueName:           "myqueue",
			AuthMode:            AzureAuthServicePrincipal,
			ServiceBusNamespace: "myns.servicebus.windows.net",
			ConnectionString:    "Endpoint=sb://...;SharedAccessKey=leftover", // must be empty
			TenantID:            "11111111-2222-3333-4444-555555555555",
			ClientID:            "c",
			ClientSecret:        "s",
		},
	}
	errs := validateAzureServiceBusConnection(conn, "test")
	require.NotEmpty(t, errs)
	assert.Contains(t, strings.Join(errs, " "), "connection_string must be empty")
}

func TestValidateAzureServiceBusConnection_SPMode_BlobSidecarInheritance(t *testing.T) {
	conn := ConnectionConfig{
		Name:                "sb",
		Provider:            ProviderAzureServiceBus,
		FileTransferEnabled: true,
		AzureServiceBus: &AzureServiceBusProviderConfig{
			QueueName:           "myqueue",
			AuthMode:            AzureAuthServicePrincipal,
			ServiceBusNamespace: "myns.servicebus.windows.net",
			TenantID:            "11111111-2222-3333-4444-555555555555",
			ClientID:            "c",
			ClientSecret:        "s",
			BlobServiceURL:      "https://a.blob.core.windows.net",
			BlobContainerName:   "filed",
			BlobAccountName:     "must-be-empty", // forbidden in SP mode
			BlobAccountKey:      "must-be-empty",
		},
	}
	errs := validateAzureServiceBusConnection(conn, "test")
	require.NotEmpty(t, errs)
	joined := strings.Join(errs, " ")
	assert.Contains(t, joined, "blob_account_name must be empty")
	assert.Contains(t, joined, "blob_account_key must be empty")
}

func TestValidateAzureServiceBusConnection_ConnectionStringMode_Legacy(t *testing.T) {
	// Existing connection-string mode keeps working (zero-config upgrade).
	conn := ConnectionConfig{
		Name:     "sb",
		Provider: ProviderAzureServiceBus,
		AzureServiceBus: &AzureServiceBusProviderConfig{
			QueueName:        "myqueue",
			ConnectionString: "Endpoint=sb://x;SharedAccessKeyName=k;SharedAccessKey=v",
		},
	}
	errs := validateAzureServiceBusConnection(conn, "test")
	assert.Empty(t, errs)
}

// ----- audit log hash prefix -----

func TestSecretHashPrefix_ConsistentAndShort(t *testing.T) {
	got := secretHashPrefix("topsec")
	assert.Len(t, got, 12)
	assert.Equal(t, got, secretHashPrefix("topsec"), "deterministic")
	assert.NotEqual(t, got, secretHashPrefix("different"))
}

// ----- buildClientSecretCredential -----

func TestBuildClientSecretCredential_HappyPath(t *testing.T) {
	cred, err := buildClientSecretCredential(azureServicePrincipalParams{
		TenantID: "11111111-2222-3333-4444-555555555555",
		ClientID: "client-uuid",
		Secret:   "topsec",
		Cloud:    cloud.AzurePublic,
	})
	require.NoError(t, err)
	require.NotNil(t, cred)
	// azidentity does not contact AAD until GetToken; constructor is local.
}

func TestBuildClientSecretCredential_BadTenant(t *testing.T) {
	// azidentity rejects empty tenant_id with a "tenantID cannot be empty" error.
	_, err := buildClientSecretCredential(azureServicePrincipalParams{
		TenantID: "",
		ClientID: "client-uuid",
		Secret:   "topsec",
		Cloud:    cloud.AzurePublic,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "azidentity NewClientSecretCredential")
}

// ----- probeAzureCredential -----

func TestProbeAzureCredential_Success(t *testing.T) {
	err := probeAzureCredential(t.Context(), stubTokenCredential{}, azureStorageScope)
	require.NoError(t, err)
}

func TestProbeAzureCredential_StubError_Sanitized(t *testing.T) {
	// Simulate an AAD failure that includes a buffered OAuth2 body. The
	// sanitizer must redact the client_secret value before the message
	// reaches the caller.
	stub := stubTokenCredential{
		err: errors.New("aad failed: post body client_secret=hunter2&grant_type=client_credentials returned 401"),
	}
	err := probeAzureCredential(t.Context(), stub, azureStorageScope)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "azure credential probe")
	assert.Contains(t, err.Error(), "client_secret=REDACTED",
		"sanitizer should redact client_secret in probe failure")
	assert.NotContains(t, err.Error(), "hunter2")
}

func TestProbeAzureCredential_ParentContextCancelled(t *testing.T) {
	// Stub blocks until external signal; cancel the parent context immediately.
	// The 10s inner timeout doesn't matter here because the parent ctx wins.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	block := make(chan struct{})
	stub := stubTokenCredential{block: block}
	err := probeAzureCredential(ctx, stub, azureStorageScope)
	require.Error(t, err)
	// Sanitizer doesn't touch "context canceled" but the error path runs.
	assert.Contains(t, err.Error(), "azure credential probe")
}

// ----- probeAzureConnectionSP dispatch -----

func TestProbeAzureConnectionSP_NotSPMode_Skip(t *testing.T) {
	// Queue in shared-key mode: no probe, return nil.
	conn := ConnectionConfig{
		Name:       "q",
		Provider:   ProviderAzureQueue,
		AzureQueue: &AzureQueueProviderConfig{AuthMode: AzureAuthSharedKey},
	}
	assert.NoError(t, probeAzureConnectionSP(t.Context(), conn))
}

func TestProbeAzureConnectionSP_NilSubConfig_Skip(t *testing.T) {
	// Defensive: nil sub-config returns nil even with SP-shaped provider.
	for _, provider := range []string{ProviderAzureQueue, ProviderAzureBlob, ProviderAzureServiceBus} {
		t.Run(provider, func(t *testing.T) {
			conn := ConnectionConfig{Name: "x", Provider: provider}
			assert.NoError(t, probeAzureConnectionSP(t.Context(), conn))
		})
	}
}

func TestProbeAzureConnectionSP_UnknownProvider_Skip(t *testing.T) {
	conn := ConnectionConfig{Name: "x", Provider: "nats"}
	assert.NoError(t, probeAzureConnectionSP(t.Context(), conn))
}

func TestProbeAzureConnectionSP_QueueSPMode_ProbesWithStorageScope(t *testing.T) {
	// SP mode + valid syntactic config + cancelled ctx: probe runs and
	// returns the connection-wrapped error from the GetToken call. We use
	// a cancelled context so the (real) azidentity credential's GetToken
	// returns context-canceled instead of hitting AAD.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	conn := ConnectionConfig{
		Name:     "queue-conn",
		Provider: ProviderAzureQueue,
		AzureQueue: &AzureQueueProviderConfig{
			AuthMode:     AzureAuthServicePrincipal,
			TenantID:     "11111111-2222-3333-4444-555555555555",
			ClientID:     "client-uuid",
			ClientSecret: "topsec",
		},
	}
	err := probeAzureConnectionSP(ctx, conn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `connection "queue-conn"`)
}

func TestProbeAzureConnectionSP_BlobSPMode_ProbesWithStorageScope(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	conn := ConnectionConfig{
		Name:     "blob-conn",
		Provider: ProviderAzureBlob,
		AzureBlob: &AzureBlobProviderConfig{
			AuthMode:     AzureAuthServicePrincipal,
			TenantID:     "11111111-2222-3333-4444-555555555555",
			ClientID:     "client-uuid",
			ClientSecret: "topsec",
		},
	}
	err := probeAzureConnectionSP(ctx, conn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `connection "blob-conn"`)
}

func TestProbeAzureConnectionSP_ServiceBusSPMode_ProbesWithServiceBusScope(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	conn := ConnectionConfig{
		Name:     "sb-conn",
		Provider: ProviderAzureServiceBus,
		AzureServiceBus: &AzureServiceBusProviderConfig{
			AuthMode:     AzureAuthServicePrincipal,
			TenantID:     "11111111-2222-3333-4444-555555555555",
			ClientID:     "client-uuid",
			ClientSecret: "topsec",
		},
	}
	err := probeAzureConnectionSP(ctx, conn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `connection "sb-conn"`)
}

// ----- probeAzureSP error wrapping -----

func TestProbeAzureSP_NoSecretSource_WrappedWithConnName(t *testing.T) {
	err := probeAzureSP(t.Context(),
		"11111111-2222-3333-4444-555555555555", "client-uuid",
		"", "", "", "", azureStorageScope, "my-conn")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `connection "my-conn"`)
	assert.Contains(t, err.Error(), "no client secret source")
}

func TestProbeAzureSP_BadCloud_WrappedWithConnName(t *testing.T) {
	err := probeAzureSP(t.Context(),
		"11111111-2222-3333-4444-555555555555", "client-uuid",
		"topsec", "", "", "atlantis", azureStorageScope, "my-conn")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `connection "my-conn"`)
	assert.Contains(t, err.Error(), "unknown azure_cloud")
}

func TestProbeAzureSP_BadTenant_WrappedWithConnName(t *testing.T) {
	err := probeAzureSP(t.Context(),
		"", "client-uuid", // empty tenant_id triggers azidentity error
		"topsec", "", "", "public", azureStorageScope, "my-conn")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `connection "my-conn"`)
}

func TestProbeAzureSP_CancelledContext_ProbeFails(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := probeAzureSP(ctx,
		"11111111-2222-3333-4444-555555555555", "client-uuid",
		"topsec", "", "", "public", azureStorageScope, "my-conn")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `connection "my-conn"`)
}

// ----- logAzureAuthAudit -----

func TestLogAzureAuthAudit_NilAPI_NoOp(t *testing.T) {
	// Must not panic on nil API.
	logAzureAuthAudit(nil, "azure-queue", "tenant", "client", "public", "secret-value")
}

func TestLogAzureAuthAudit_EmitsExpectedFields(t *testing.T) {
	api := &plugintest.API{}
	defer api.AssertExpectations(t)

	// Capture the call so we can introspect args.
	var capturedArgs []any
	api.On("LogInfo", "Azure auth (service-principal): credential constructed",
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			for _, a := range args {
				capturedArgs = append(capturedArgs, a)
			}
		}).Return().Once()

	logAzureAuthAudit(api, "azure-queue",
		"11111111-2222-3333-4444-555555555555", "client-uuid",
		"usgov", "very-secret-value")

	// Reassemble as a single string for substring checks.
	combined := ""
	for _, a := range capturedArgs {
		if s, ok := a.(string); ok {
			combined += s + " "
		}
	}
	assert.Contains(t, combined, "azure-queue", "provider")
	assert.Contains(t, combined, "service-principal", "auth_mode")
	assert.Contains(t, combined, "11111111-2222-3333-4444-555555555555", "tenant_id")
	assert.Contains(t, combined, "client-uuid", "client_id")
	assert.Contains(t, combined, "usgov", "normalized azure_cloud")
	// Secret hash prefix should appear, raw secret must NOT.
	assert.Contains(t, combined, secretHashPrefix("very-secret-value"))
	assert.NotContains(t, combined, "very-secret-value",
		"raw secret value must NEVER appear in audit log")
}

func TestLogAzureAuthAudit_EmptyCloudNormalizes(t *testing.T) {
	api := &plugintest.API{}
	defer api.AssertExpectations(t)
	api.On("LogInfo", mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			// args[12] is the value for "azure_cloud" key (kv-pair order
			// matches the source).
			require.Len(t, args, 15)
			assert.Equal(t, "public", args[12], "empty cloud normalizes to public")
		}).Return().Once()
	logAzureAuthAudit(api, "azure-blob", "tenant", "client", "", "secret")
}

// ----- normalizeAzureCloudForLog -----

func TestNormalizeAzureCloudForLog(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", AzureCloudPublic},
		{"public", "public"},
		{"PUBLIC", "public"},
		{"  usgov  ", "usgov"},
		{"China", "china"},
		{"unknown", "unknown"}, // not validated here; just normalized
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, normalizeAzureCloudForLog(tc.in))
		})
	}
}
