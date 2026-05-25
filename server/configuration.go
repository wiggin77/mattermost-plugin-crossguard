package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
)

var validNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// tenantIDGUIDPattern matches the GUID form of an Azure AD tenant.
// azidentity also accepts FQDN forms (e.g. contoso.onmicrosoft.com); see
// validateAzureTenantID for the full acceptance rule.
var tenantIDGUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

const subjectPrefix = "crossguard."

const (
	AuthTypeNone        = "none"
	AuthTypeToken       = "token"
	AuthTypeCredentials = "credentials"

	fileFilterModeAllow = "allow"
	fileFilterModeDeny  = "deny"

	ProviderNATS            = "nats"
	ProviderAzureQueue      = "azure-queue"
	ProviderAzureBlob       = "azure-blob"
	ProviderAzureServiceBus = "azure-servicebus"

	// Azure auth modes selected via the per-provider `auth_mode` field.
	// Empty string defaults to the legacy mode for that provider:
	// shared-key for Queue/Blob, connection-string for Service Bus.
	AzureAuthSharedKey        = "shared-key"
	AzureAuthConnectionString = "connection-string"
	AzureAuthServicePrincipal = "service-principal"

	// Azure cloud environments. Drives both the AAD authority host (via
	// azidentity ClientOptions.Cloud) and the data-plane SDK client options
	// for Queue and Blob. Service Bus routing comes from the FQDN namespace.
	AzureCloudPublic = "public"
	AzureCloudUSGov  = "usgov"
	AzureCloudChina  = "china"

	// SecretSentinel is the placeholder value the webapp sends back for an
	// unchanged secret field. OnConfigurationChange preserves the stored
	// value when it sees this string, so the cleartext secret never has to
	// round-trip through the browser. Deliberately distinctive (not a row
	// of asterisks) so it cannot collide with a real secret an operator
	// might paste, and is unambiguous in logs and config dumps.
	SecretSentinel = "__CROSSGUARD_SECRET_UNCHANGED__" //nolint:gosec // not a real credential; sentinel for unchanged-secret form round-trip
)

// azureCloudValues enumerates the accepted azure_cloud config values for
// validation. Keep in sync with the constants above.
var azureCloudValues = []string{AzureCloudPublic, AzureCloudUSGov, AzureCloudChina}

// normalizeAzureAuthMode trims whitespace and lowercases the input so that
// validation always sees the canonical form. Empty stays empty (defaults to
// legacy mode at the validation site).
func normalizeAzureAuthMode(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// validateAzureTenantID accepts either a GUID or an FQDN-shaped tenant
// identifier. Returns the empty string when valid, or a human-readable
// reason when not. azidentity.NewClientSecretCredential accepts both
// forms, so the validator must too; rejecting domain-form tenants would
// break legitimate "contoso.onmicrosoft.com" deployments.
func validateAzureTenantID(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return "tenant_id is required"
	}
	if tenantIDGUIDPattern.MatchString(t) {
		return ""
	}
	// FQDN-shape: contains a dot, no scheme, no path, no whitespace.
	if !strings.Contains(t, ".") || strings.ContainsAny(t, " \t/\\") || strings.Contains(t, "://") {
		return "tenant_id must be a GUID or FQDN-shaped (e.g. contoso.onmicrosoft.com)"
	}
	return ""
}

// validateServiceBusNamespace accepts an FQDN like myns.servicebus.windows.net.
// If the operator typed `sb://...`, the scheme is stripped before checking;
// the validator returns the cleaned form via the second return value so the
// caller can normalize.
func validateServiceBusNamespace(s string) (cleaned string, problem string) {
	t := strings.TrimSpace(s)
	if t == "" {
		return "", "service_bus_namespace is required"
	}
	t = strings.TrimPrefix(t, "sb://")
	t = strings.TrimSuffix(t, "/")
	if strings.ContainsAny(t, " \t/\\") || !strings.Contains(t, ".") {
		return "", "service_bus_namespace must be a fully-qualified namespace (e.g. myns.servicebus.windows.net)"
	}
	return t, ""
}

// validateAzureCloud returns "" if the value is empty or one of the allowed
// constants; otherwise an error message.
func validateAzureCloud(s string) string {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" {
		return ""
	}
	if slices.Contains(azureCloudValues, t) {
		return ""
	}
	return fmt.Sprintf("azure_cloud must be one of %s, %s, or %s", AzureCloudPublic, AzureCloudUSGov, AzureCloudChina)
}

// validateAzureServicePrincipalFields checks the SP credential triple.
// Returns a slice of human-readable errors (empty if all good).
//
// Inline-only by design: operators who want to keep the secret out of
// plugin config use Mattermost's MM_PLUGINSETTINGS_PLUGINS_CROSSGUARD_*
// env-var substitution on the whole connections JSON, which is the same
// mechanism every other plugin secret in Mattermost uses.
func validateAzureServicePrincipalFields(tenantID, clientID, secret, prefix string) []string {
	var errs []string
	if msg := validateAzureTenantID(tenantID); msg != "" {
		errs = append(errs, fmt.Sprintf("%s: %s", prefix, msg))
	}
	if strings.TrimSpace(clientID) == "" {
		errs = append(errs, fmt.Sprintf("%s: client_id is required", prefix))
	}
	if strings.TrimSpace(secret) == "" {
		errs = append(errs, fmt.Sprintf("%s: client_secret is required", prefix))
	}
	return errs
}

// azureForbidSPFields returns an error for every populated SP field. Used
// in legacy auth-mode branches to enforce mutual exclusion: leftover SP
// creds in shared-key / connection-string mode is silent stale-state.
func azureForbidSPFields(tenantID, clientID, secret, prefix, mode string) []string {
	var errs []string
	check := func(field, value string) {
		if strings.TrimSpace(value) != "" {
			errs = append(errs, fmt.Sprintf("%s: %s must be empty when auth_mode is %s", prefix, field, mode))
		}
	}
	check("tenant_id", tenantID)
	check("client_id", clientID)
	check("client_secret", secret)
	return errs
}

var (
	errMissingNATSConfig            = errors.New("nats config block is required when provider is \"nats\"")
	errMissingAzureQueueConfig      = errors.New("azure_queue config block is required when provider is \"azure-queue\"")
	errMissingAzureBlobConfig       = errors.New("azure_blob config block is required when provider is \"azure-blob\"")
	errMissingAzureServiceBusConfig = errors.New("azure_servicebus config block is required when provider is \"azure-servicebus\"")
)

func errUnknownProvider(p string) error {
	return fmt.Errorf("unknown provider %q, must be \"nats\", \"azure-queue\", \"azure-blob\", or \"azure-servicebus\"", p)
}

// ConnectionConfig represents a single connection configuration.
// It supports both NATS and Azure providers via nested sub-structs.
type ConnectionConfig struct {
	Name     string `json:"name"`
	Provider string `json:"provider"` // "nats", "azure-queue", or "azure-blob"

	// SiteURL is the stable identity used to register the connection as a
	// shared channels remote on the server. It defaults to "crossguard:" + Name
	// on first save, and is immutable thereafter so renames preserve the
	// remote's identity (and its sync cursor). Inbound and outbound
	// connections that share a SiteURL are paired and share a single
	// remoteID for loop prevention and synthetic user tagging.
	SiteURL string `json:"site_url,omitempty"`

	// MessageFormat selects the outbound wire encoding: "xml" (default)
	// or "json". Empty defaults to "xml" at publish time. Ignored on
	// inbound connections, which auto-detect the format from the first
	// non-whitespace byte of each incoming envelope.
	MessageFormat string `json:"message_format,omitempty"`

	// Common fields
	FileTransferEnabled bool   `json:"file_transfer_enabled"`
	FileFilterMode      string `json:"file_filter_mode"`  // "", "allow", "deny"
	FileFilterTypes     string `json:"file_filter_types"` // ".pdf,.docx,.png"

	// Provider-specific (exactly one must be set, matching Provider)
	NATS            *NATSProviderConfig            `json:"nats,omitempty"`
	AzureQueue      *AzureQueueProviderConfig      `json:"azure_queue,omitempty"`
	AzureBlob       *AzureBlobProviderConfig       `json:"azure_blob,omitempty"`
	AzureServiceBus *AzureServiceBusProviderConfig `json:"azure_servicebus,omitempty"`
}

// NATSProviderConfig holds NATS-specific connection settings.
type NATSProviderConfig struct {
	Name       string `json:"name,omitempty"` // populated from parent at parse time
	Address    string `json:"address"`
	Subject    string `json:"subject"`
	TLSEnabled bool   `json:"tls_enabled"`
	AuthType   string `json:"auth_type"`
	Token      string `json:"token"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	ClientCert string `json:"client_cert"`
	ClientKey  string `json:"client_key"`
	CACert     string `json:"ca_cert"`
}

// AzureQueueProviderConfig holds Azure Queue Storage and Blob Storage connection settings.
//
// Auth shape: AuthMode selects between shared-key (legacy default) and
// service-principal (Client Secret). In service-principal mode AccountKey
// must be empty and TenantID/ClientID/ClientSecret are required.
type AzureQueueProviderConfig struct {
	QueueServiceURL         string `json:"queue_service_url"`
	BlobServiceURL          string `json:"blob_service_url"`
	AccountName             string `json:"account_name"`
	AccountKey              string `json:"account_key"`
	QueueName               string `json:"queue_name"`
	BlobContainerName       string `json:"blob_container_name"`
	PollIntervalSeconds     int    `json:"poll_interval_seconds,omitempty"`      // default 5
	BlobPollIntervalSeconds int    `json:"blob_poll_interval_seconds,omitempty"` // default 15

	// AuthMode: "" (defaults to shared-key) | "shared-key" | "service-principal"
	AuthMode string `json:"auth_mode,omitempty"`

	// AzureCloud: "" (defaults to public) | "public" | "usgov" | "china"
	AzureCloud string `json:"azure_cloud,omitempty"`

	// Service Principal fields (required when AuthMode == "service-principal").
	// ClientSecret is the inline secret value. To keep the secret out of
	// plugin config on disk, inject the whole connections JSON via the
	// MM_PLUGINSETTINGS_PLUGINS_CROSSGUARD_OUTBOUNDCONNECTIONS env var.
	TenantID     string `json:"tenant_id,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
}

// AzureServiceBusProviderConfig holds Azure Service Bus queue + optional Azure Blob file transfer settings.
//
// Auth shape: AuthMode selects between connection-string (legacy default,
// SAS-based) and service-principal. In service-principal mode the
// ConnectionString field must be empty and ServiceBusNamespace +
// TenantID/ClientID/ClientSecret are required. The blob sidecar inherits
// the parent SP credential when AuthMode == "service-principal"; in that
// case BlobAccountName and BlobAccountKey must be empty (sidecar
// mixed-mode is deferred). Message lock duration is NOT a client knob
// (it is an Azure entity property set on the queue definition), so there
// is no corresponding field here.
type AzureServiceBusProviderConfig struct {
	ConnectionString string `json:"connection_string,omitempty"`
	QueueName        string `json:"queue_name"`

	// Optional Blob sidecar for file transfer (only populated when the parent
	// ConnectionConfig has FileTransferEnabled=true). Service Bus is
	// message-only; files flow through a separate Blob Storage container.
	BlobServiceURL    string `json:"blob_service_url,omitempty"`
	BlobAccountName   string `json:"blob_account_name,omitempty"`
	BlobAccountKey    string `json:"blob_account_key,omitempty"`
	BlobContainerName string `json:"blob_container_name,omitempty"`

	// Tunables (optional). Everything else (batch size 32, receive max-wait 5 s,
	// ack timeout 10 s) is a package-level const.
	MaxMessageSizeBytes     int `json:"max_message_size_bytes,omitempty"`     // default 192000; Premium can raise to 100 MiB
	BlobPollIntervalSeconds int `json:"blob_poll_interval_seconds,omitempty"` // default 15 (mirrors azure-queue)

	// AuthMode: "" (defaults to connection-string) | "connection-string" | "service-principal"
	AuthMode string `json:"auth_mode,omitempty"`

	// AzureCloud: "" (defaults to public) | "public" | "usgov" | "china".
	// For Service Bus, this is passed only to the credential options;
	// data-plane routing uses ServiceBusNamespace.
	AzureCloud string `json:"azure_cloud,omitempty"`

	// ServiceBusNamespace is the fully-qualified Service Bus namespace
	// (e.g. "myns.servicebus.windows.net" or
	// "myns.servicebus.usgovcloudapi.net"). Required when AuthMode ==
	// "service-principal" because no SAS connection string encodes it.
	ServiceBusNamespace string `json:"service_bus_namespace,omitempty"`

	// Service Principal fields (required when AuthMode == "service-principal").
	TenantID     string `json:"tenant_id,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
}

// AzureBlobProviderConfig holds Azure Blob Storage provider settings for batch message relay.
//
// Auth shape: AuthMode selects between shared-key (legacy default) and
// service-principal (Client Secret). In service-principal mode AccountKey
// must be empty and TenantID/ClientID/ClientSecret are required.
type AzureBlobProviderConfig struct {
	ServiceURL               string `json:"service_url"`
	AccountName              string `json:"account_name"`
	AccountKey               string `json:"account_key"`
	BlobContainerName        string `json:"blob_container_name"`
	FlushIntervalSeconds     int    `json:"flush_interval_seconds,omitempty"`      // default 60
	BlobLockMaxAgeSeconds    int    `json:"blob_lock_max_age_seconds,omitempty"`   // default 300 (5 min)
	BatchPollIntervalSeconds int    `json:"batch_poll_interval_seconds,omitempty"` // default 30

	// AuthMode: "" (defaults to shared-key) | "shared-key" | "service-principal"
	AuthMode string `json:"auth_mode,omitempty"`

	// AzureCloud: "" (defaults to public) | "public" | "usgov" | "china"
	AzureCloud string `json:"azure_cloud,omitempty"`

	// Service Principal fields (required when AuthMode == "service-principal").
	TenantID     string `json:"tenant_id,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
}

func isFileAllowed(filename, filterMode, filterTypes string) bool {
	if filterMode == "" {
		return true
	}

	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		ext = "."
	}

	types := parseFilterTypes(filterTypes)
	found := slices.Contains(types, ext)

	if filterMode == fileFilterModeAllow {
		return found
	}
	// deny mode
	return !found
}

func parseFilterTypes(raw string) []string {
	var types []string
	for part := range strings.SplitSeq(raw, ",") {
		t := strings.TrimSpace(strings.ToLower(part))
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, ".") {
			t = "." + t
		}
		types = append(types, t)
	}
	return types
}

type configuration struct {
	InboundConnections        string `json:"InboundConnections"`
	OutboundConnections       string `json:"OutboundConnections"`
	RestrictToSystemAdmins    *bool  `json:"RestrictToSystemAdmins"`
	AllowTeamAdminRequests    *bool  `json:"AllowTeamAdminRequests"`
	AllowChannelAdminRequests *bool  `json:"AllowChannelAdminRequests"`

	// Sequencer (Phase 4) knobs. Zero values fall back to the defaults
	// declared in sequencerDefaultGapTimeoutSeconds /
	// sequencerDefaultBufferMaxEnvelopes / sequencerDefaultBufferMaxBytes.
	SequencerGapTimeoutSeconds  int `json:"SequencerGapTimeoutSeconds"`
	SequencerBufferMaxEnvelopes int `json:"SequencerBufferMaxEnvelopes"`
	SequencerBufferMaxBytes     int `json:"SequencerBufferMaxBytes"`
}

const (
	sequencerDefaultGapTimeoutSeconds  = 30
	sequencerDefaultBufferMaxEnvelopes = 200
	sequencerDefaultBufferMaxBytes     = 50 * 1024 * 1024
)

func (c *configuration) gapTimeout() time.Duration {
	if c.SequencerGapTimeoutSeconds <= 0 {
		return sequencerDefaultGapTimeoutSeconds * time.Second
	}
	return time.Duration(c.SequencerGapTimeoutSeconds) * time.Second
}

func (c *configuration) bufferMaxEnvelopes() int {
	if c.SequencerBufferMaxEnvelopes <= 0 {
		return sequencerDefaultBufferMaxEnvelopes
	}
	return c.SequencerBufferMaxEnvelopes
}

func (c *configuration) bufferMaxBytes() int {
	if c.SequencerBufferMaxBytes <= 0 {
		return sequencerDefaultBufferMaxBytes
	}
	return c.SequencerBufferMaxBytes
}

func (c *configuration) isRestrictedToSystemAdmins() bool {
	return c.RestrictToSystemAdmins != nil && *c.RestrictToSystemAdmins
}

func (c *configuration) isTeamAdminRequestsAllowed() bool {
	return c.AllowTeamAdminRequests != nil && *c.AllowTeamAdminRequests
}

func (c *configuration) isRequestMode() bool {
	return c.isRestrictedToSystemAdmins() && c.isTeamAdminRequestsAllowed()
}

func (c *configuration) isChannelAdminRequestsAllowed() bool {
	return c.AllowChannelAdminRequests != nil && *c.AllowChannelAdminRequests
}

func (c *configuration) isChannelRequestMode() bool {
	return c.isRestrictedToSystemAdmins() && c.isChannelAdminRequestsAllowed()
}

func parseConnections(raw string) ([]ConnectionConfig, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || trimmed == "[]" {
		return nil, nil
	}

	var connections []ConnectionConfig
	if err := json.Unmarshal([]byte(trimmed), &connections); err != nil {
		return nil, fmt.Errorf("failed to parse connections: %w", err)
	}

	// Default provider to NATS, populate nested name field, and default
	// SiteURL when empty. SiteURL defaulting happens here (not just in
	// validate) because every reader of the config goes through this
	// function, including registerRemotes and reconcileRemotes. The
	// admin console's saved JSON is the source of truth: a SiteURL
	// already present in saved JSON survives renames; an empty SiteURL
	// is stamped on every read so the runtime always has one.
	for i := range connections {
		if connections[i].Provider == "" {
			connections[i].Provider = ProviderNATS
		}
		if connections[i].NATS != nil {
			connections[i].NATS.Name = connections[i].Name
		}
		if strings.TrimSpace(connections[i].SiteURL) == "" {
			name := strings.TrimSpace(connections[i].Name)
			if name != "" {
				connections[i].SiteURL = "crossguard:" + name
			}
		}
	}

	return connections, nil
}

func (c *configuration) GetInboundConnections() ([]ConnectionConfig, error) {
	return parseConnections(c.InboundConnections)
}

func (c *configuration) GetOutboundConnections() ([]ConnectionConfig, error) {
	return parseConnections(c.OutboundConnections)
}

func (c *configuration) validate() error {
	var errs []string

	inbound, err := c.GetInboundConnections()
	if err != nil {
		errs = append(errs, fmt.Sprintf("inbound connections: %s", err.Error()))
	} else {
		inboundNames := make(map[string]bool)
		errs = append(errs, validateConnectionList(inbound, "inbound", inboundNames)...)
	}

	outbound, err := c.GetOutboundConnections()
	if err != nil {
		errs = append(errs, fmt.Sprintf("outbound connections: %s", err.Error()))
	} else {
		outboundNames := make(map[string]bool)
		errs = append(errs, validateConnectionList(outbound, "outbound", outboundNames)...)
	}

	if len(errs) > 0 {
		return fmt.Errorf("configuration errors: %s", strings.Join(errs, "; "))
	}

	return nil
}

func validateConnectionList(connections []ConnectionConfig, direction string, allNames map[string]bool) []string {
	var errs []string

	for i, conn := range connections {
		prefix := fmt.Sprintf("%s connection %d", direction, i)

		trimmedName := strings.TrimSpace(conn.Name)
		switch {
		case trimmedName == "":
			errs = append(errs, fmt.Sprintf("%s: name is required", prefix))
		case !validNamePattern.MatchString(trimmedName):
			errs = append(errs, fmt.Sprintf("%s: name must contain only lowercase letters, numbers, and hyphens", prefix))
		case allNames[trimmedName]:
			errs = append(errs, fmt.Sprintf("%s: duplicate name %q", prefix, trimmedName))
		default:
			allNames[trimmedName] = true
		}

		switch conn.Provider {
		case ProviderNATS, "":
			errs = append(errs, validateNATSConnection(conn, prefix)...)
		case ProviderAzureQueue:
			errs = append(errs, validateAzureQueueConnection(conn, prefix)...)
		case ProviderAzureBlob:
			errs = append(errs, validateAzureBlobConnection(conn, prefix)...)
		case ProviderAzureServiceBus:
			errs = append(errs, validateAzureServiceBusConnection(conn, prefix)...)
		default:
			errs = append(errs, fmt.Sprintf("%s: provider must be \"nats\", \"azure-queue\", \"azure-blob\", or \"azure-servicebus\"", prefix))
		}

		// SiteURL defaulting happens at parse time (see parseConnections),
		// so we do not need to populate it here. Once saved by the admin,
		// SiteURL travels with the connection across renames.

		switch conn.FileFilterMode {
		case "", fileFilterModeAllow, fileFilterModeDeny:
			// valid
		default:
			errs = append(errs, fmt.Sprintf("%s: file_filter_mode must be \"\", \"allow\", or \"deny\"", prefix))
		}

		if (conn.FileFilterMode == fileFilterModeAllow || conn.FileFilterMode == fileFilterModeDeny) && strings.TrimSpace(conn.FileFilterTypes) == "" {
			errs = append(errs, fmt.Sprintf("%s: file_filter_types is required when file_filter_mode is set", prefix))
		}

		switch strings.ToLower(strings.TrimSpace(conn.MessageFormat)) {
		case "", "xml", "json":
			// valid; empty defaults to "xml" at publish time
		default:
			errs = append(errs, fmt.Sprintf("%s: message_format must be \"xml\" or \"json\"", prefix))
		}
	}

	return errs
}

func validateNATSConnection(conn ConnectionConfig, prefix string) []string {
	var errs []string

	if conn.NATS == nil {
		errs = append(errs, fmt.Sprintf("%s: nats config block is required when provider is \"nats\"", prefix))
		return errs
	}

	nats := conn.NATS

	if strings.TrimSpace(nats.Address) == "" {
		errs = append(errs, fmt.Sprintf("%s: address is required", prefix))
	}

	trimmedSubject := strings.TrimSpace(nats.Subject)
	switch {
	case trimmedSubject == "":
		errs = append(errs, fmt.Sprintf("%s: subject is required", prefix))
	case !strings.HasPrefix(trimmedSubject, subjectPrefix):
		errs = append(errs, fmt.Sprintf("%s: subject must start with %q", prefix, subjectPrefix))
	}

	switch nats.AuthType {
	case AuthTypeNone, AuthTypeToken, AuthTypeCredentials, "":
		// valid
	default:
		errs = append(errs, fmt.Sprintf("%s: auth_type must be \"none\", \"token\", or \"credentials\"", prefix))
	}

	if nats.AuthType == AuthTypeToken && strings.TrimSpace(nats.Token) == "" {
		errs = append(errs, fmt.Sprintf("%s: token is required when auth_type is \"token\"", prefix))
	}

	if nats.AuthType == AuthTypeCredentials {
		if strings.TrimSpace(nats.Username) == "" {
			errs = append(errs, fmt.Sprintf("%s: username is required when auth_type is \"credentials\"", prefix))
		}
		if strings.TrimSpace(nats.Password) == "" {
			errs = append(errs, fmt.Sprintf("%s: password is required when auth_type is \"credentials\"", prefix))
		}
	}

	hasCert := nats.ClientCert != ""
	hasKey := nats.ClientKey != ""
	if hasCert != hasKey {
		errs = append(errs, fmt.Sprintf("%s: both client_cert and client_key must be provided together", prefix))
	}

	return errs
}

func validateAzureQueueConnection(conn ConnectionConfig, prefix string) []string {
	var errs []string

	if conn.AzureQueue == nil {
		errs = append(errs, fmt.Sprintf("%s: azure_queue config block is required when provider is \"azure-queue\"", prefix))
		return errs
	}

	az := conn.AzureQueue

	// URL + queue name + cloud env apply to every auth mode.
	if strings.TrimSpace(az.QueueServiceURL) == "" {
		errs = append(errs, fmt.Sprintf("%s: queue_service_url is required", prefix))
	} else if _, err := url.Parse(az.QueueServiceURL); err != nil {
		errs = append(errs, fmt.Sprintf("%s: queue_service_url is not a valid URL: %v", prefix, err))
	}

	if conn.FileTransferEnabled {
		if strings.TrimSpace(az.BlobServiceURL) == "" {
			errs = append(errs, fmt.Sprintf("%s: blob_service_url is required when file_transfer_enabled is true", prefix))
		} else if _, err := url.Parse(az.BlobServiceURL); err != nil {
			errs = append(errs, fmt.Sprintf("%s: blob_service_url is not a valid URL: %v", prefix, err))
		}
	}

	if strings.TrimSpace(az.QueueName) == "" {
		errs = append(errs, fmt.Sprintf("%s: queue_name is required", prefix))
	}

	if conn.FileTransferEnabled && strings.TrimSpace(az.BlobContainerName) == "" {
		errs = append(errs, fmt.Sprintf("%s: blob_container_name is required when file_transfer_enabled is true", prefix))
	}

	if msg := validateAzureCloud(az.AzureCloud); msg != "" {
		errs = append(errs, fmt.Sprintf("%s: %s", prefix, msg))
	}

	// Per-auth-mode required and forbidden fields. Empty AuthMode defaults
	// to the legacy mode (shared-key) so existing configs migrate cleanly.
	switch normalizeAzureAuthMode(az.AuthMode) {
	case "", AzureAuthSharedKey:
		if strings.TrimSpace(az.AccountName) == "" {
			errs = append(errs, fmt.Sprintf("%s: account_name is required for shared-key auth_mode", prefix))
		}
		if strings.TrimSpace(az.AccountKey) == "" {
			errs = append(errs, fmt.Sprintf("%s: account_key is required for shared-key auth_mode", prefix))
		}
		errs = append(errs, azureForbidSPFields(az.TenantID, az.ClientID, az.ClientSecret, prefix, "shared-key")...)
	case AzureAuthServicePrincipal:
		if strings.TrimSpace(az.AccountKey) != "" {
			errs = append(errs, fmt.Sprintf("%s: account_key must be empty when auth_mode is service-principal", prefix))
		}
		errs = append(errs, validateAzureServicePrincipalFields(az.TenantID, az.ClientID, az.ClientSecret, prefix)...)
	default:
		errs = append(errs, fmt.Sprintf("%s: auth_mode must be %q or %q (got %q)", prefix, AzureAuthSharedKey, AzureAuthServicePrincipal, az.AuthMode))
	}

	errs = append(errs, validatePollInterval(az.PollIntervalSeconds, prefix, "poll_interval_seconds")...)
	errs = append(errs, validatePollInterval(az.BlobPollIntervalSeconds, prefix, "blob_poll_interval_seconds")...)

	return errs
}

// pollIntervalMaxSeconds is the upper bound for configurable poll intervals.
// Operators who want a longer interval should rethink their architecture;
// this bound is defense against config typos that would silently stall a
// connection.
const pollIntervalMaxSeconds = 3600

func validatePollInterval(value int, prefix, field string) []string {
	if value == 0 {
		return nil
	}
	var errs []string
	if value < 1 {
		errs = append(errs, fmt.Sprintf("%s: %s must be at least 1", prefix, field))
	}
	if value > pollIntervalMaxSeconds {
		errs = append(errs, fmt.Sprintf("%s: %s must be at most %d", prefix, field, pollIntervalMaxSeconds))
	}
	return errs
}

func validateAzureBlobConnection(conn ConnectionConfig, prefix string) []string {
	var errs []string

	if conn.AzureBlob == nil {
		errs = append(errs, fmt.Sprintf("%s: azure_blob config block is required when provider is \"azure-blob\"", prefix))
		return errs
	}

	ab := conn.AzureBlob

	if strings.TrimSpace(ab.ServiceURL) == "" {
		errs = append(errs, fmt.Sprintf("%s: service_url is required", prefix))
	} else if _, err := url.Parse(ab.ServiceURL); err != nil {
		errs = append(errs, fmt.Sprintf("%s: service_url is not a valid URL: %v", prefix, err))
	}

	if strings.TrimSpace(ab.BlobContainerName) == "" {
		errs = append(errs, fmt.Sprintf("%s: blob_container_name is required", prefix))
	}

	if msg := validateAzureCloud(ab.AzureCloud); msg != "" {
		errs = append(errs, fmt.Sprintf("%s: %s", prefix, msg))
	}

	switch normalizeAzureAuthMode(ab.AuthMode) {
	case "", AzureAuthSharedKey:
		if strings.TrimSpace(ab.AccountName) == "" {
			errs = append(errs, fmt.Sprintf("%s: account_name is required for shared-key auth_mode", prefix))
		}
		if strings.TrimSpace(ab.AccountKey) == "" {
			errs = append(errs, fmt.Sprintf("%s: account_key is required for shared-key auth_mode", prefix))
		}
		errs = append(errs, azureForbidSPFields(ab.TenantID, ab.ClientID, ab.ClientSecret, prefix, "shared-key")...)
	case AzureAuthServicePrincipal:
		if strings.TrimSpace(ab.AccountKey) != "" {
			errs = append(errs, fmt.Sprintf("%s: account_key must be empty when auth_mode is service-principal", prefix))
		}
		errs = append(errs, validateAzureServicePrincipalFields(ab.TenantID, ab.ClientID, ab.ClientSecret, prefix)...)
	default:
		errs = append(errs, fmt.Sprintf("%s: auth_mode must be %q or %q (got %q)", prefix, AzureAuthSharedKey, AzureAuthServicePrincipal, ab.AuthMode))
	}

	if ab.FlushIntervalSeconds != 0 && ab.FlushIntervalSeconds < 5 {
		errs = append(errs, fmt.Sprintf("%s: flush_interval_seconds must be at least 5", prefix))
	}

	if ab.BlobLockMaxAgeSeconds != 0 {
		if ab.BlobLockMaxAgeSeconds < 30 {
			errs = append(errs, fmt.Sprintf("%s: blob_lock_max_age_seconds must be at least 30", prefix))
		}
		if ab.BlobLockMaxAgeSeconds > int(blobLockMaxAgeCap/time.Second) {
			errs = append(errs, fmt.Sprintf("%s: blob_lock_max_age_seconds must be at most %d",
				prefix, int(blobLockMaxAgeCap/time.Second)))
		}
	}

	errs = append(errs, validatePollInterval(ab.BatchPollIntervalSeconds, prefix, "batch_poll_interval_seconds")...)

	return errs
}

// serviceBusQueueNamePattern matches valid Azure Service Bus queue paths: letters,
// digits, periods, hyphens, underscores, and forward slashes (hierarchical paths).
// Length is capped at 260 characters by Service Bus itself.
var serviceBusQueueNamePattern = regexp.MustCompile(`^[A-Za-z0-9._/\-]+$`)

const serviceBusQueueNameMaxLen = 260

// serviceBusMaxMessageSizeBytesCap is the hard upper bound for the
// MaxMessageSizeBytes config knob: 100 MiB matches the Service Bus Premium
// tier's published single-message ceiling.
const serviceBusMaxMessageSizeBytesCap = 100 * 1024 * 1024

// serviceBusMinMessageSizeBytes guards against typos that would reject every
// realistic payload.
const serviceBusMinMessageSizeBytes = 1024

func validateAzureServiceBusConnection(conn ConnectionConfig, prefix string) []string {
	var errs []string

	if conn.AzureServiceBus == nil {
		errs = append(errs, fmt.Sprintf("%s: azure_servicebus config block is required when provider is \"azure-servicebus\"", prefix))
		return errs
	}

	sb := conn.AzureServiceBus

	trimmedQueue := strings.TrimSpace(sb.QueueName)
	switch {
	case trimmedQueue == "":
		errs = append(errs, fmt.Sprintf("%s: queue_name is required", prefix))
	case len(trimmedQueue) > serviceBusQueueNameMaxLen:
		errs = append(errs, fmt.Sprintf("%s: queue_name must be at most %d characters", prefix, serviceBusQueueNameMaxLen))
	case !serviceBusQueueNamePattern.MatchString(trimmedQueue):
		errs = append(errs, fmt.Sprintf("%s: queue_name may contain only letters, digits, periods, hyphens, underscores, and forward slashes", prefix))
	}

	if msg := validateAzureCloud(sb.AzureCloud); msg != "" {
		errs = append(errs, fmt.Sprintf("%s: %s", prefix, msg))
	}

	// Service Bus auth mode: empty defaults to connection-string (legacy).
	// In SP mode ServiceBusNamespace is required and the sidecar blob
	// shared-key fields must be empty (sidecar inherits the parent SP).
	authMode := normalizeAzureAuthMode(sb.AuthMode)
	switch authMode {
	case "", AzureAuthConnectionString:
		if strings.TrimSpace(sb.ConnectionString) == "" {
			errs = append(errs, fmt.Sprintf("%s: connection_string is required for connection-string auth_mode", prefix))
		}
		if strings.TrimSpace(sb.ServiceBusNamespace) != "" {
			errs = append(errs, fmt.Sprintf("%s: service_bus_namespace must be empty when auth_mode is connection-string", prefix))
		}
		errs = append(errs, azureForbidSPFields(sb.TenantID, sb.ClientID, sb.ClientSecret, prefix, "connection-string")...)
	case AzureAuthServicePrincipal:
		if strings.TrimSpace(sb.ConnectionString) != "" {
			errs = append(errs, fmt.Sprintf("%s: connection_string must be empty when auth_mode is service-principal", prefix))
		}
		if _, msg := validateServiceBusNamespace(sb.ServiceBusNamespace); msg != "" {
			errs = append(errs, fmt.Sprintf("%s: %s", prefix, msg))
		}
		errs = append(errs, validateAzureServicePrincipalFields(sb.TenantID, sb.ClientID, sb.ClientSecret, prefix)...)
	default:
		errs = append(errs, fmt.Sprintf("%s: auth_mode must be %q or %q (got %q)", prefix, AzureAuthConnectionString, AzureAuthServicePrincipal, sb.AuthMode))
	}

	if conn.FileTransferEnabled {
		if strings.TrimSpace(sb.BlobServiceURL) == "" {
			errs = append(errs, fmt.Sprintf("%s: blob_service_url is required when file_transfer_enabled is true", prefix))
		} else if _, err := url.Parse(sb.BlobServiceURL); err != nil {
			errs = append(errs, fmt.Sprintf("%s: blob_service_url is not a valid URL: %v", prefix, err))
		}
		if strings.TrimSpace(sb.BlobContainerName) == "" {
			errs = append(errs, fmt.Sprintf("%s: blob_container_name is required when file_transfer_enabled is true", prefix))
		}
		// connection-string mode requires an independent blob shared-key
		// credential when file transfer is on. SP mode inherits the parent
		// SP credential, so the blob_account_* fields are forbidden in SP
		// mode regardless of file_transfer_enabled (see SP-mode block below).
		if authMode == "" || authMode == AzureAuthConnectionString {
			if strings.TrimSpace(sb.BlobAccountName) == "" {
				errs = append(errs, fmt.Sprintf("%s: blob_account_name is required when file_transfer_enabled is true", prefix))
			}
			if strings.TrimSpace(sb.BlobAccountKey) == "" {
				errs = append(errs, fmt.Sprintf("%s: blob_account_key is required when file_transfer_enabled is true", prefix))
			}
		}
	}

	// SP-mode forbid check for the blob sidecar shared-key fields applies
	// even when file_transfer_enabled is false. Otherwise an admin who
	// switches off file transfer leaves stale BlobAccountKey material in
	// stored config (the sentinel-merge path would even rehydrate it).
	if authMode == AzureAuthServicePrincipal {
		if strings.TrimSpace(sb.BlobAccountName) != "" {
			errs = append(errs, fmt.Sprintf("%s: blob_account_name must be empty when auth_mode is service-principal (sidecar inherits parent SP credential)", prefix))
		}
		if strings.TrimSpace(sb.BlobAccountKey) != "" {
			errs = append(errs, fmt.Sprintf("%s: blob_account_key must be empty when auth_mode is service-principal (sidecar inherits parent SP credential)", prefix))
		}
	}

	if sb.MaxMessageSizeBytes != 0 {
		if sb.MaxMessageSizeBytes < serviceBusMinMessageSizeBytes {
			errs = append(errs, fmt.Sprintf("%s: max_message_size_bytes must be at least %d", prefix, serviceBusMinMessageSizeBytes))
		}
		if sb.MaxMessageSizeBytes > serviceBusMaxMessageSizeBytesCap {
			errs = append(errs, fmt.Sprintf("%s: max_message_size_bytes must be at most %d", prefix, serviceBusMaxMessageSizeBytesCap))
		}
	}

	errs = append(errs, validatePollInterval(sb.BlobPollIntervalSeconds, prefix, "blob_poll_interval_seconds")...)

	return errs
}

// isTestMessage returns the TestID and true when data is a TransportEnvelope
// of Type "test". Used by NATS file watcher and inbound paths to recognize
// test-connection probes. The wire-format detection is delegated to
// UnmarshalEnvelope so the function correctly accepts both XML and JSON
// test envelopes.
func isTestMessage(data []byte) (string, bool) {
	env, _, err := UnmarshalEnvelope(data)
	if err != nil {
		return "", false
	}
	if env.Type != TransportTypeTest {
		return "", false
	}
	return env.TestID, true
}

func (p *Plugin) getConfiguration() *configuration {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()

	if p.configuration == nil {
		return &configuration{}
	}

	return p.configuration
}

func (p *Plugin) setConfiguration(configuration *configuration) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()

	if configuration != nil && p.configuration == configuration {
		if p.API != nil {
			p.API.LogWarn("setConfiguration called with the existing configuration",
				"error_code", errcode.ConfigSameConfigPassed)
		}
		return
	}

	p.configuration = configuration
}

func (p *Plugin) OnConfigurationChange() error {
	cfg := new(configuration)

	if err := p.API.LoadPluginConfiguration(cfg); err != nil {
		return fmt.Errorf("failed to load plugin configuration: %w", err)
	}

	// Secret hydration sentinel merge. Before validation runs, replace any
	// SecretSentinel placeholder in the inbound config with the stored
	// value from the currently-loaded configuration. This is the save-path
	// defense: an admin who reopens the System Console and clicks Save
	// without re-entering passwords keeps the stored values. An empty
	// inbound value is honored as an explicit clear so admins can actually
	// delete a stored secret. See mergeSecretsFromPrevious for the rules.
	if mergeErr := p.mergeSecretsFromPrevious(cfg); mergeErr != nil {
		p.API.LogWarn("Plugin configuration: secret sentinel merge failed",
			"error_code", errcode.ConfigValidationWarn,
			"error", mergeErr.Error())
		return fmt.Errorf("plugin configuration merge failed: %w", mergeErr)
	}

	// Validation failures abort the configuration apply by returning an
	// error to Mattermost. Previously these were logged as warnings and
	// the bad config was applied anyway, which (a) silently accepted
	// malformed configs and (b) gave Service Principal credential probe
	// failures no way to block the save. The error path is the contract:
	// Mattermost will reject the config save and surface the message to
	// the admin.
	if err := cfg.validate(); err != nil {
		p.API.LogWarn("Plugin configuration rejected: validation failed",
			"error_code", errcode.ConfigValidationWarn,
			"error", err.Error())
		return fmt.Errorf("plugin configuration validation failed: %w", err)
	}

	// Save-time Service Principal probe. For every Azure connection in SP
	// mode, acquire a token against the provider's data-plane scope. Bad
	// tenant/client/secret values surface here as a clear save error
	// rather than as repeated mid-relay token failures.
	if err := p.probeAzureSPConnections(cfg); err != nil {
		return fmt.Errorf("plugin configuration validation failed: %w", err)
	}

	p.setConfiguration(cfg)

	// p.ctx is set in OnActivate, so a non-nil ctx means activation has run
	// and it is safe to reconnect providers and reconcile remotes. Calls to
	// OnConfigurationChange that arrive before activation must skip these
	// steps because the providers have not been built yet.
	if p.ctx != nil {
		p.reconnectOutbound()
		p.reconnectInbound()
		p.reconcileRemotes()
	}

	return nil
}

// mergeSecretsFromPrevious resolves the secret-hydration sentinel for
// per-connection secret fields. For each secret field, the inbound rule
// is:
//
//	inbound == SecretSentinel  -> keep stored value (form open, untouched)
//	inbound != SecretSentinel  -> accept inbound (empty means explicit clear)
//
// The merge writes back into the inbound configuration's JSON strings so
// downstream validation and probe see the resolved values. The fields
// covered are: ClientSecret (Queue, Blob, ServiceBus), AccountKey (Queue,
// Blob), BlobAccountKey (ServiceBus), and ConnectionString (ServiceBus).
//
// Returns an error only if parsing the inbound JSON fails. A corrupt
// stored list is non-fatal because the new config replaces it anyway; we
// log loudly so operators can correlate.
func (p *Plugin) mergeSecretsFromPrevious(cfg *configuration) error {
	prev := p.getConfiguration()
	if prev == nil {
		return nil
	}

	for _, field := range []struct {
		raw      string
		prevRaw  string
		writeTo  func(string)
		listName string
	}{
		{cfg.InboundConnections, prev.InboundConnections, func(s string) { cfg.InboundConnections = s }, "inbound"},
		{cfg.OutboundConnections, prev.OutboundConnections, func(s string) { cfg.OutboundConnections = s }, "outbound"},
	} {
		if strings.TrimSpace(field.raw) == "" || field.raw == "[]" {
			continue
		}
		merged, prevParseErr, err := mergeConnectionSecretsJSON(field.raw, field.prevRaw)
		if err != nil {
			return fmt.Errorf("%s connections: %w", field.listName, err)
		}
		if prevParseErr != nil {
			// The stored config's connection list is unparseable. Secrets
			// are NOT preserved for this list; admin will see "required"
			// errors for any sentinel field unless they re-paste. Route
			// through sanitizeAzureError defensively in case a partially-
			// quoted SAS key appears in the error context.
			p.API.LogWarn("Plugin configuration: stored connections list is corrupt; secret sentinel merge could not preserve previous values",
				"error_code", errcode.ConfigValidationWarn,
				"list", field.listName, "error", sanitizeAzureError(prevParseErr))
		}
		field.writeTo(merged)
	}
	return nil
}

// mergeConnectionSecretsJSON parses both the inbound and previous-stored
// connection lists, applies the sentinel-merge rules per secret field,
// and returns the inbound JSON with secrets resolved. The previous list
// is name-keyed; missing names contribute no overrides.
//
// Returns three values: the merged JSON, a prev-parse error (non-nil
// when the stored config is corrupt, caller logs and proceeds), and a
// fatal error (non-nil when the inbound list itself is unparseable or
// re-marshaling fails).
func mergeConnectionSecretsJSON(inboundRaw, prevRaw string) (merged string, prevParseErr, err error) {
	inboundList, err := parseConnections(inboundRaw)
	if err != nil {
		return "", nil, fmt.Errorf("parse inbound: %w", err)
	}
	prevList, parseErr := parseConnections(prevRaw)
	if parseErr != nil {
		prevParseErr = parseErr
	}

	prevByName := make(map[string]ConnectionConfig, len(prevList))
	for _, c := range prevList {
		prevByName[c.Name] = c
	}

	for i := range inboundList {
		stored, ok := prevByName[inboundList[i].Name]
		if !ok {
			// New connection (or prev was corrupt); sentinel resolves to
			// empty so the validator surfaces "required" rather than
			// accepting the sentinel literal as a real secret.
			clearSentinelSecrets(&inboundList[i])
			continue
		}
		mergeOneConnectionSecrets(&inboundList[i], &stored)
	}

	out, err := json.Marshal(inboundList)
	if err != nil {
		return "", prevParseErr, fmt.Errorf("marshal merged: %w", err)
	}
	return string(out), prevParseErr, nil
}

// mergeOneConnectionSecrets applies the per-field merge rules between an
// inbound connection and its stored predecessor. Mutates the inbound
// argument in place.
func mergeOneConnectionSecrets(inbound, stored *ConnectionConfig) {
	if inbound.AzureQueue != nil && stored.AzureQueue != nil {
		inbound.AzureQueue.AccountKey = preserveSecret(inbound.AzureQueue.AccountKey, stored.AzureQueue.AccountKey)
		inbound.AzureQueue.ClientSecret = preserveSecret(inbound.AzureQueue.ClientSecret, stored.AzureQueue.ClientSecret)
	}
	if inbound.AzureBlob != nil && stored.AzureBlob != nil {
		inbound.AzureBlob.AccountKey = preserveSecret(inbound.AzureBlob.AccountKey, stored.AzureBlob.AccountKey)
		inbound.AzureBlob.ClientSecret = preserveSecret(inbound.AzureBlob.ClientSecret, stored.AzureBlob.ClientSecret)
	}
	if inbound.AzureServiceBus != nil && stored.AzureServiceBus != nil {
		inbound.AzureServiceBus.ConnectionString = preserveSecret(inbound.AzureServiceBus.ConnectionString, stored.AzureServiceBus.ConnectionString)
		inbound.AzureServiceBus.BlobAccountKey = preserveSecret(inbound.AzureServiceBus.BlobAccountKey, stored.AzureServiceBus.BlobAccountKey)
		inbound.AzureServiceBus.ClientSecret = preserveSecret(inbound.AzureServiceBus.ClientSecret, stored.AzureServiceBus.ClientSecret)
	}
	clearSentinelSecrets(inbound)
}

// preserveSecret implements the sentinel-merge rule for a single field:
//   - inbound == sentinel -> keep stored (admin reopened the form, did not edit)
//   - inbound != sentinel -> accept inbound (whatever the admin typed)
//
// Empty inbound means "the admin cleared this field"; we honor that so an
// admin can actually delete a stored secret (e.g., when transitioning
// from shared-key to service-principal). The webapp uses the sentinel
// for "no change", so an empty inbound here is an explicit clear.
func preserveSecret(inbound, stored string) string {
	if inbound == SecretSentinel {
		return stored
	}
	return inbound
}

// clearSentinelSecrets removes any leftover sentinel values when there is
// no stored predecessor. The validator should treat such fields as truly
// empty so it can surface "required" errors rather than accept the
// sentinel string as a real secret.
func clearSentinelSecrets(c *ConnectionConfig) {
	if c.AzureQueue != nil {
		if c.AzureQueue.AccountKey == SecretSentinel {
			c.AzureQueue.AccountKey = ""
		}
		if c.AzureQueue.ClientSecret == SecretSentinel {
			c.AzureQueue.ClientSecret = ""
		}
	}
	if c.AzureBlob != nil {
		if c.AzureBlob.AccountKey == SecretSentinel {
			c.AzureBlob.AccountKey = ""
		}
		if c.AzureBlob.ClientSecret == SecretSentinel {
			c.AzureBlob.ClientSecret = ""
		}
	}
	if c.AzureServiceBus != nil {
		if c.AzureServiceBus.ConnectionString == SecretSentinel {
			c.AzureServiceBus.ConnectionString = ""
		}
		if c.AzureServiceBus.BlobAccountKey == SecretSentinel {
			c.AzureServiceBus.BlobAccountKey = ""
		}
		if c.AzureServiceBus.ClientSecret == SecretSentinel {
			c.AzureServiceBus.ClientSecret = ""
		}
	}
}

// probeAzureSPConnections walks inbound and outbound connections and runs
// the save-time GetToken probe for each one in SP mode. Returns the first
// probe failure (sanitized) so the config save is rejected and the admin
// sees a clear actionable message.
//
// Each probe is bounded by azureCredentialProbeTimeout (10s) inside
// probeAzureCredential, so this function uses context.Background() rather
// than reading p.ctx. After a disable/enable cycle p.ctx is non-nil but
// already cancelled, which would make every probe fail with "context
// canceled"; a save-time probe also doesn't benefit from plugin-lifetime
// cancellation because the operation is synchronous and short.
func (p *Plugin) probeAzureSPConnections(cfg *configuration) error {
	ctx := context.Background()

	inbound, err := cfg.GetInboundConnections()
	if err != nil {
		return fmt.Errorf("inbound connections: %w", err)
	}
	for _, conn := range inbound {
		if probeErr := probeAzureConnectionSP(ctx, conn); probeErr != nil {
			return probeErr
		}
	}

	outbound, err := cfg.GetOutboundConnections()
	if err != nil {
		return fmt.Errorf("outbound connections: %w", err)
	}
	for _, conn := range outbound {
		if probeErr := probeAzureConnectionSP(ctx, conn); probeErr != nil {
			return probeErr
		}
	}

	return nil
}
