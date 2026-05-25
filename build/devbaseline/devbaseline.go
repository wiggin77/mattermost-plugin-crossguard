// Package devbaseline defines the connection set provisioned on the
// dual-server Docker dev environment by `make docker-deploy`. It is the
// single source of truth for what connections exist where, shared by:
//
//   - build/configure-baseline (the CLI that emits the JSON config-patch
//     body the Makefile sends to each server)
//   - server/integration tests (which reference connections by name when
//     issuing init-team / init-channel slash commands)
//
// The package has no build tag so it can be imported from both the build
// tooling and from integration tests (whose own files are guarded by
// //go:build integration). It also pulls in no runtime plugin code, so
// the build helper does not transitively compile the plugin.
//
// Wire format: no connection in this baseline declares an explicit
// message_format. Every outbound connection defaults to XML at
// publish time; `make docker-integration-test-validate-wire` overrides
// every outbound connection's message_format to the chosen
// WIRE_FORMAT (xml or json) before running the suite. Integration
// tests therefore exercise whichever format the run was invoked
// with, never a hardcoded one.
package devbaseline

// Connection names. Each name identifies a connection that lives on both
// Server A and Server B (outbound on the sender, inbound on the receiver).
// Tests that exercise a given transport use the corresponding name in
// /crossguard init-team and init-channel slash commands.
//
// One connection per transport keeps the dev baseline compact. Tests
// achieve per-test isolation via dedicated channel names and dedicated
// poster users, not via dedicated connections.
const (
	NATSLowToHighName       = "low-to-high"
	NATSHighToLowName       = "high-to-low"
	AzureQueueLowToHighName = "azure-low-to-high"
	AzureBlobLowToHighName  = "azure-blob-low-to-high"
	ServiceBusLowToHighName = "servicebus-low-to-high"
)

// Side identifies one of the two dev Mattermost instances.
type Side string

const (
	SideA Side = "a"
	SideB Side = "b"
)

// Dev fixture endpoints. All hostnames are container-internal, reachable
// from the plugin running inside the mattermost-a / mattermost-b
// containers. The Azurite credentials are the well-known dev account from
// the Azure Storage emulator; not secret.
const (
	natsAddress = "nats://nats:4222"

	azuriteAccountName = "devstoreaccount1"
	//nolint:gosec // G101: well-known Azurite emulator key, not a real secret
	azuriteAccountKey  = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="
	azuriteQueueURL    = "http://azurite:10001/devstoreaccount1"
	azuriteBlobURL     = "http://azurite:10000/devstoreaccount1"
	azureQueueName     = "crossguard-azure-test"
	azureQueueBlobName = "crossguard-azure-files"
	azureBlobBatchName = "crossguard-azure-blob-batches"

	servicebusConnStr   = "Endpoint=sb://servicebus-emulator;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=SAS_KEY_VALUE;UseDevelopmentEmulator=true"
	servicebusQueueName = "crossguard-relay"
	servicebusBlobName  = "crossguard-servicebus-files"
)

// natsConn builds a NATS connection config map. fileTransfer toggles
// file_transfer_enabled. No message_format is set: the wire format is
// controlled at deploy/test time via configure-baseline's -format flag.
func natsConn(name, subject string, fileTransfer bool) map[string]any {
	return map[string]any{
		"name":                  name,
		"provider":              "nats",
		"file_transfer_enabled": fileTransfer,
		"nats": map[string]any{
			"address":   natsAddress,
			"subject":   subject,
			"auth_type": "none",
		},
	}
}

func azureQueueConn(name string) map[string]any {
	return map[string]any{
		"name":                  name,
		"provider":              "azure-queue",
		"file_transfer_enabled": true,
		"azure_queue": map[string]any{
			"queue_service_url":          azuriteQueueURL,
			"blob_service_url":           azuriteBlobURL,
			"account_name":               azuriteAccountName,
			"account_key":                azuriteAccountKey,
			"queue_name":                 azureQueueName,
			"blob_container_name":        azureQueueBlobName,
			"poll_interval_seconds":      1,
			"blob_poll_interval_seconds": 1,
		},
	}
}

func azureBlobConn(name string) map[string]any {
	return map[string]any{
		"name":                  name,
		"provider":              "azure-blob",
		"file_transfer_enabled": true,
		"azure_blob": map[string]any{
			"service_url":                 azuriteBlobURL,
			"account_name":                azuriteAccountName,
			"account_key":                 azuriteAccountKey,
			"blob_container_name":         azureBlobBatchName,
			"flush_interval_seconds":      5,
			"batch_poll_interval_seconds": 1,
		},
	}
}

func servicebusConn(name string) map[string]any {
	return map[string]any{
		"name":                  name,
		"provider":              "azure-servicebus",
		"file_transfer_enabled": true,
		"azure_servicebus": map[string]any{
			"connection_string":          servicebusConnStr,
			"queue_name":                 servicebusQueueName,
			"blob_service_url":           azuriteBlobURL,
			"blob_account_name":          azuriteAccountName,
			"blob_account_key":           azuriteAccountKey,
			"blob_container_name":        servicebusBlobName,
			"blob_poll_interval_seconds": 1,
		},
	}
}

// Outbound returns the outbound connection list for the given dev side.
// Server A is the sender for the low-to-high family (one NATS, plus the
// three Azure providers) and the reverse-direction receiver for
// high-to-low. Server B is the inverse.
func Outbound(side Side) []map[string]any {
	if side == SideA {
		return []map[string]any{
			natsConn(NATSLowToHighName, "crossguard.relay", true),
			azureQueueConn(AzureQueueLowToHighName),
			azureBlobConn(AzureBlobLowToHighName),
			servicebusConn(ServiceBusLowToHighName),
		}
	}
	return []map[string]any{
		natsConn(NATSHighToLowName, "crossguard.relay.reverse", true),
	}
}

// Inbound returns the inbound connection list for the given dev side.
func Inbound(side Side) []map[string]any {
	if side == SideA {
		return []map[string]any{
			natsConn(NATSHighToLowName, "crossguard.relay.reverse", true),
		}
	}
	return []map[string]any{
		natsConn(NATSLowToHighName, "crossguard.relay", true),
		azureQueueConn(AzureQueueLowToHighName),
		azureBlobConn(AzureBlobLowToHighName),
		servicebusConn(ServiceBusLowToHighName),
	}
}
