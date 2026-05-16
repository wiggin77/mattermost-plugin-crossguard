package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ----- mergeConnectionSecretsJSON -----

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

func TestMergeConnectionSecretsJSON_Sentinel_PreservesStored(t *testing.T) {
	prev := []ConnectionConfig{{
		Name:     "q1",
		Provider: ProviderAzureQueue,
		AzureQueue: &AzureQueueProviderConfig{
			AccountName: "acct", AccountKey: "real-stored-key",
		},
	}}
	inbound := []ConnectionConfig{{
		Name:     "q1",
		Provider: ProviderAzureQueue,
		AzureQueue: &AzureQueueProviderConfig{
			AccountName: "acct", AccountKey: SecretSentinel, // unchanged from form load
		},
	}}
	merged, _, err := mergeConnectionSecretsJSON(mustJSON(t, inbound), mustJSON(t, prev))
	require.NoError(t, err)
	assert.Contains(t, merged, "real-stored-key", "sentinel should resolve to stored value")
	assert.NotContains(t, merged, SecretSentinel, "sentinel should be replaced")
}

func TestMergeConnectionSecretsJSON_EmptyInbound_ClearsStored(t *testing.T) {
	// Empty inbound is now honored as an explicit clear so admins can
	// actually delete a stored secret (e.g., when switching from
	// shared-key to service-principal). The webapp uses the sentinel
	// for "no change"; empty truly means "I cleared this field".
	prev := []ConnectionConfig{{
		Name:     "q1",
		Provider: ProviderAzureQueue,
		AzureQueue: &AzureQueueProviderConfig{
			AccountName: "acct", AccountKey: "real-stored-key",
		},
	}}
	inbound := []ConnectionConfig{{
		Name:     "q1",
		Provider: ProviderAzureQueue,
		AzureQueue: &AzureQueueProviderConfig{
			AccountName: "acct", AccountKey: "", // explicit clear
		},
	}}
	merged, _, err := mergeConnectionSecretsJSON(mustJSON(t, inbound), mustJSON(t, prev))
	require.NoError(t, err)
	assert.NotContains(t, merged, "real-stored-key", "empty inbound clears stored")
}

func TestMergeConnectionSecretsJSON_NewValue_AcceptsInbound(t *testing.T) {
	prev := []ConnectionConfig{{
		Name:     "q1",
		Provider: ProviderAzureQueue,
		AzureQueue: &AzureQueueProviderConfig{
			AccountName: "acct", AccountKey: "old-key",
		},
	}}
	inbound := []ConnectionConfig{{
		Name:     "q1",
		Provider: ProviderAzureQueue,
		AzureQueue: &AzureQueueProviderConfig{
			AccountName: "acct", AccountKey: "freshly-edited-key",
		},
	}}
	merged, _, err := mergeConnectionSecretsJSON(mustJSON(t, inbound), mustJSON(t, prev))
	require.NoError(t, err)
	assert.Contains(t, merged, "freshly-edited-key", "real edit accepted")
	assert.NotContains(t, merged, "old-key", "old value replaced")
}

func TestMergeConnectionSecretsJSON_NewConnection_SentinelClearedToEmpty(t *testing.T) {
	// A brand-new connection with no stored predecessor should NOT have the
	// sentinel preserved (otherwise the validator would accept "********"
	// as a real secret).
	inbound := []ConnectionConfig{{
		Name:     "new-conn",
		Provider: ProviderAzureQueue,
		AzureQueue: &AzureQueueProviderConfig{
			AccountName: "acct", AccountKey: SecretSentinel,
		},
	}}
	merged, _, err := mergeConnectionSecretsJSON(mustJSON(t, inbound), "[]")
	require.NoError(t, err)
	assert.NotContains(t, merged, SecretSentinel, "sentinel cleared on new connection")
	// The marshaled output uses "account_key":"" (or omits omitempty); ensure
	// no leftover sentinel string remains.
}

func TestMergeConnectionSecretsJSON_ServiceBus_AllSecretFields(t *testing.T) {
	prev := []ConnectionConfig{{
		Name:     "sb1",
		Provider: ProviderAzureServiceBus,
		AzureServiceBus: &AzureServiceBusProviderConfig{
			ConnectionString: "Endpoint=sb://x;SharedAccessKey=stored-conn-key",
			BlobAccountKey:   "stored-blob-key",
			ClientSecret:     "stored-client-secret",
		},
	}}
	inbound := []ConnectionConfig{{
		Name:     "sb1",
		Provider: ProviderAzureServiceBus,
		AzureServiceBus: &AzureServiceBusProviderConfig{
			// All three secret fields use the sentinel = "no change".
			ConnectionString: SecretSentinel,
			BlobAccountKey:   SecretSentinel,
			ClientSecret:     SecretSentinel,
		},
	}}
	merged, _, err := mergeConnectionSecretsJSON(mustJSON(t, inbound), mustJSON(t, prev))
	require.NoError(t, err)
	assert.Contains(t, merged, "stored-conn-key")
	assert.Contains(t, merged, "stored-blob-key")
	assert.Contains(t, merged, "stored-client-secret")
}

func TestMergeConnectionSecretsJSON_CorruptPrev_NonFatal(t *testing.T) {
	// When the stored connections JSON is unparseable (e.g., manual edit of
	// config.json, partial write, schema downgrade), the merge must continue
	// non-fatally - the function returns prevParseErr != nil but err == nil
	// so the caller can LogWarn and proceed. The merged result for an
	// inbound connection with a sentinel resolves to empty (no stored
	// counterpart to inherit from), forcing the validator to surface
	// "required" errors instead of silently accepting the sentinel literal.
	inbound := []ConnectionConfig{{
		Name:     "q1",
		Provider: ProviderAzureQueue,
		AzureQueue: &AzureQueueProviderConfig{
			AccountName: "acct", AccountKey: SecretSentinel,
		},
	}}
	corruptPrev := `[{"name":"q1","azure_queue":{` // truncated JSON, unterminated
	merged, prevParseErr, err := mergeConnectionSecretsJSON(mustJSON(t, inbound), corruptPrev)
	require.NoError(t, err, "corrupt prev must NOT be a fatal error")
	require.Error(t, prevParseErr, "corrupt prev must surface a non-fatal error so caller can LogWarn")
	assert.NotContains(t, merged, SecretSentinel, "sentinel cleared when no stored counterpart")
}

func TestMergeConnectionSecretsJSON_DifferentName_NoMerge(t *testing.T) {
	// A connection whose name doesn't match any stored entry should be left
	// alone (sentinel cleared but not preserved).
	prev := []ConnectionConfig{{
		Name:     "old-name",
		Provider: ProviderAzureQueue,
		AzureQueue: &AzureQueueProviderConfig{
			AccountKey: "stored",
		},
	}}
	inbound := []ConnectionConfig{{
		Name:     "different-name",
		Provider: ProviderAzureQueue,
		AzureQueue: &AzureQueueProviderConfig{
			AccountKey: "fresh-input",
		},
	}}
	merged, _, err := mergeConnectionSecretsJSON(mustJSON(t, inbound), mustJSON(t, prev))
	require.NoError(t, err)
	assert.Contains(t, merged, "fresh-input")
	assert.NotContains(t, merged, "stored", "name mismatch must not inherit stored secret")
}

// ----- preserveSecret unit table -----

func TestPreserveSecret(t *testing.T) {
	cases := []struct {
		name, inbound, stored, want string
	}{
		{"sentinel keeps stored", SecretSentinel, "real-key", "real-key"},
		{"empty inbound clears stored", "", "real-key", ""},
		{"new value accepted", "new-key", "old-key", "new-key"},
		{"both empty stays empty", "", "", ""},
		{"sentinel with empty stored is empty", SecretSentinel, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := preserveSecret(tc.inbound, tc.stored)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ----- redactConnection no-secrets contract -----

func TestRedactConnection_NoSecretsExposed_AzureQueue(t *testing.T) {
	conn := ConnectionConfig{
		Name:     "q1",
		Provider: ProviderAzureQueue,
		AzureQueue: &AzureQueueProviderConfig{
			AccountName:  "acct",
			AccountKey:   "SECRET-account-key",
			AuthMode:     AzureAuthServicePrincipal,
			TenantID:     "11111111-2222-3333-4444-555555555555",
			ClientID:     "client-uuid",
			ClientSecret: "SECRET-client-secret",
			QueueName:    "q",
		},
	}
	rc := redactConnection(conn, "outbound")
	out, err := json.Marshal(rc)
	require.NoError(t, err)
	s := string(out)
	assert.NotContains(t, s, "SECRET-account-key")
	assert.NotContains(t, s, "SECRET-client-secret")
	// Safe fields are present.
	assert.Contains(t, s, "service-principal")
	assert.Contains(t, s, "11111111-2222-3333-4444-555555555555")
	assert.Contains(t, s, "client-uuid")
}

func TestRedactConnection_NoSecretsExposed_AzureServiceBus(t *testing.T) {
	conn := ConnectionConfig{
		Name:     "sb1",
		Provider: ProviderAzureServiceBus,
		AzureServiceBus: &AzureServiceBusProviderConfig{
			ConnectionString:    "Endpoint=sb://x;SharedAccessKey=LEAKY",
			BlobAccountKey:      "SECRET-blob-key",
			ClientSecret:        "SECRET-client-secret",
			AuthMode:            AzureAuthServicePrincipal,
			ServiceBusNamespace: "myns.servicebus.windows.net",
			TenantID:            "11111111-2222-3333-4444-555555555555",
			ClientID:            "client-uuid",
			AzureCloud:          AzureCloudUSGov,
		},
	}
	rc := redactConnection(conn, "outbound")
	s := mustJSON(t, rc)
	assert.NotContains(t, s, "LEAKY")
	assert.NotContains(t, s, "SECRET-blob-key")
	assert.NotContains(t, s, "SECRET-client-secret")
	assert.Contains(t, s, "myns.servicebus.windows.net")
	assert.Contains(t, s, AzureCloudUSGov)
	assert.True(t, strings.Contains(s, "service-principal"))
}

// ----- wrapAzureResourceError 404 detection -----

func TestWrapAzureResourceError_404SPMode_GetsHint(t *testing.T) {
	err := wrapAzureResourceError(
		errAzureNotFoundLike("RESPONSE 404: ContainerNotFound; The specified container does not exist."),
		"container", "mycontainer", AzureAuthServicePrincipal,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Pre-provision")
	assert.Contains(t, err.Error(), "mycontainer")
}

func TestWrapAzureResourceError_404SharedKey_Milder(t *testing.T) {
	err := wrapAzureResourceError(
		errAzureNotFoundLike("RESPONSE 404: ContainerNotFound; The specified container does not exist."),
		"container", "mycontainer", AzureAuthSharedKey,
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.NotContains(t, err.Error(), "Pre-provision")
}

func TestWrapAzureResourceError_NonResourceError_ReturnsAsIs(t *testing.T) {
	err := wrapAzureResourceError(
		errAzureNotFoundLike("network: connection refused"),
		"queue", "q1", AzureAuthServicePrincipal,
	)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "Pre-provision")
	assert.Contains(t, err.Error(), "connection refused")
}

func TestWrapAzureResourceError_NilReturnsNil(t *testing.T) {
	assert.NoError(t, wrapAzureResourceError(nil, "queue", "q1", AzureAuthSharedKey))
}

// errAzureNotFoundLike is a tiny helper for the tests above.
type notFoundErr struct{ msg string }

func (e notFoundErr) Error() string { return e.msg }

func errAzureNotFoundLike(msg string) error { return notFoundErr{msg: msg} }
