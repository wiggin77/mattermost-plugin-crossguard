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
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
)

var validNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// tenantIDGUIDPattern matches the GUID form of an Azure AD tenant.
// azidentity also accepts FQDN forms (e.g. contoso.onmicrosoft.com); see
// validateAzureTenantID for the full acceptance rule.
var tenantIDGUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

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
// the validator returns the cleaned form via the second return value so
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

// validateAzureCloud returns "" if value is empty or one of the allowed
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
// env-var substitution on the whole connections JSON, which is the
// same mechanism every other plugin secret in Mattermost uses.
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
	// unchanged secret field. OnConfigurationChange preserves the stored value
	// when it sees this string, so the cleartext secret never has to round-trip.
	// The token is deliberately distinctive (not a row of asterisks) so it
	// cannot collide with a real secret an operator might paste, and so it is
	// unambiguous in logs and config dumps.
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

	// Common fields
	FileTransferEnabled bool   `json:"file_transfer_enabled"`
	FileFilterMode      string `json:"file_filter_mode"`  // "", "allow", "deny"
	FileFilterTypes     string `json:"file_filter_types"` // ".pdf,.docx,.png"
	MessageFormat       string `json:"message_format"`    // "json" or "xml"

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
// must be empty and TenantID/ClientID/ClientSecret* are required.
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
// case BlobAccountName and BlobAccountKey must be empty (mixed-mode is
// Phase 2). Message lock duration is NOT a client knob (it is an Azure
// entity property set on the queue definition), so there is no
// corresponding field here.
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

	// AzureCloud: "" (defaults to public) | "public" | "usgov" | "china"
	// For Service Bus, this is passed only to the credential options;
	// data-plane routing uses ServiceBusNamespace.
	AzureCloud string `json:"azure_cloud,omitempty"`

	// ServiceBusNamespace is the fully-qualified Service Bus namespace
	// (e.g. "myns.servicebus.windows.net" or "myns.servicebus.usgovcloudapi.net").
	// Required when AuthMode == "service-principal" because the SAS
	// connection string used to encode this.
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
	UsernameLookup            *bool  `json:"UsernameLookup"`
	RestrictToSystemAdmins    *bool  `json:"RestrictToSystemAdmins"`
	AllowTeamAdminRequests    *bool  `json:"AllowTeamAdminRequests"`
	AllowChannelAdminRequests *bool  `json:"AllowChannelAdminRequests"`
}

func (c *configuration) isUsernameLookupEnabled() bool {
	return c.UsernameLookup == nil || *c.UsernameLookup
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

	// Default provider to NATS and populate nested name field.
	for i := range connections {
		if connections[i].Provider == "" {
			connections[i].Provider = ProviderNATS
		}
		if connections[i].NATS != nil {
			connections[i].NATS.Name = connections[i].Name
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

	// Uniqueness is per-direction, not global. The operative identifier
	// everywhere (slash commands, kvstore keys at the service layer,
	// status responses) is the direction-qualified "outbound:name" /
	// "inbound:name" pair, so the same bare name in both lists is a
	// legitimate loopback configuration where a server publishes and
	// subscribes on a single subject (see the loopback integration test).
	inbound, err := c.GetInboundConnections()
	if err != nil {
		errs = append(errs, fmt.Sprintf("inbound connections: %s", err.Error()))
	} else {
		errs = append(errs, validateConnectionList(inbound, "inbound", make(map[string]bool))...)
	}

	outbound, err := c.GetOutboundConnections()
	if err != nil {
		errs = append(errs, fmt.Sprintf("outbound connections: %s", err.Error()))
	} else {
		errs = append(errs, validateConnectionList(outbound, "outbound", make(map[string]bool))...)
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

		if conn.MessageFormat == "" {
			connections[i].MessageFormat = "json"
			conn.MessageFormat = "json"
		}
		switch conn.MessageFormat {
		case "json", "xml":
			// valid
		default:
			errs = append(errs, fmt.Sprintf("%s: message_format must be \"json\" or \"xml\"", prefix))
		}

		switch conn.FileFilterMode {
		case "", fileFilterModeAllow, fileFilterModeDeny:
			// valid
		default:
			errs = append(errs, fmt.Sprintf("%s: file_filter_mode must be \"\", \"allow\", or \"deny\"", prefix))
		}

		if (conn.FileFilterMode == fileFilterModeAllow || conn.FileFilterMode == fileFilterModeDeny) && strings.TrimSpace(conn.FileFilterTypes) == "" {
			errs = append(errs, fmt.Sprintf("%s: file_filter_types is required when file_filter_mode is set", prefix))
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
		// Shared-key mode (legacy default): account_name + account_key required;
		// SP fields must be empty.
		if strings.TrimSpace(az.AccountName) == "" {
			errs = append(errs, fmt.Sprintf("%s: account_name is required for shared-key auth_mode", prefix))
		}
		if strings.TrimSpace(az.AccountKey) == "" {
			errs = append(errs, fmt.Sprintf("%s: account_key is required for shared-key auth_mode", prefix))
		}
		errs = append(errs, azureForbidSPFields(az.TenantID, az.ClientID, az.ClientSecret, prefix, "shared-key")...)
	case AzureAuthServicePrincipal:
		// SP mode: SP triple required; shared-key fields must be empty.
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

// azureForbidSPFields returns an error for every populated SP field. Used in
// legacy auth-mode branches to enforce mutual exclusion: leftover SP creds
// in shared-key mode is silent stale-state that the plan explicitly forbids.
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
	// In SP mode, ServiceBusNamespace is required and the sidecar blob
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
		cleanedNs, msg := validateServiceBusNamespace(sb.ServiceBusNamespace)
		if msg != "" {
			errs = append(errs, fmt.Sprintf("%s: %s", prefix, msg))
		}
		_ = cleanedNs // normalization applied at construction time
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
		// mode regardless of file_transfer_enabled (see the SP block below).
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

func isTestMessage(data []byte) (*model.TestMessage, bool) {
	format := model.DetectFormat(data)
	env, err := model.Unmarshal(data, format)
	if err != nil {
		return nil, false
	}

	if env.Type != model.MessageTypeTest || env.TestMessage == nil {
		return nil, false
	}

	return env.TestMessage, true
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

	// Secret hydration sentinel merge. Before validation runs, replace the
	// SecretSentinel placeholder ("__CROSSGUARD_SECRET_UNCHANGED__") in the
	// inbound config with the values from the currently-loaded plugin
	// configuration. This is the SAVE-path defense: an admin who reopens
	// the System Console and clicks Save without re-entering passwords
	// keeps the stored values. An empty inbound value is honored as an
	// explicit clear so admins can actually delete a stored secret.
	// See p.mergeSecretsFromPrevious for the rules.
	if mergeErr := p.mergeSecretsFromPrevious(cfg); mergeErr != nil {
		p.API.LogWarn("Plugin configuration: secret sentinel merge failed",
			"error_code", errcode.ConfigValidationWarn,
			"error", mergeErr.Error())
		return fmt.Errorf("plugin configuration merge failed: %w", mergeErr)
	}

	// Validation failures now ABORT the configuration apply by returning
	// an error to Mattermost. Previously these were logged as warnings and
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
	// tenant/client/secret values surface here as a clear save error rather
	// than as repeated mid-relay token failures. Errors are sanitized.
	if err := p.probeAzureSPConnections(cfg); err != nil {
		return fmt.Errorf("plugin configuration validation failed: %w", err)
	}

	p.setConfiguration(cfg)

	if p.retryQueue != nil {
		p.retryQueue.SetMaxAge(p.computeRetryMaxAge())
	}

	if p.relaySem != nil {
		p.reconnectOutbound()
		p.reconnectInbound()
	}

	return nil
}

// mergeSecretsFromPrevious resolves the secret-hydration sentinel for
// per-connection secret fields. For each secret field, the inbound rule is:
//
//	inbound == SecretSentinel  -> keep stored value (form open, untouched)
//	inbound != SecretSentinel  -> accept inbound (empty means explicit clear)
//
// The merge writes back into the inbound configuration's JSON strings so
// downstream validation and probe see the resolved values. The fields
// covered are: ClientSecret (Queue, Blob, ServiceBus), AccountKey (Queue,
// Blob), BlobAccountKey (ServiceBus), and ConnectionString (ServiceBus).
//
// Returns an error only if parsing the inbound or stored JSON fails. The
// merge itself is name-keyed: a stored connection with no matching name
// in the inbound config contributes nothing.
func (p *Plugin) mergeSecretsFromPrevious(cfg *configuration) error {
	prev := p.getConfiguration()
	if prev == nil {
		return nil // first-ever load; nothing to merge
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
			// The stored config's connection list is unparseable. The merge
			// returned an empty-map result so secrets are NOT preserved for
			// this list; admin will see "required" errors for any sentinel
			// field unless they re-paste. Log loudly so operators can
			// correlate the confusing error with the underlying cause.
			// Route through sanitizeAzureError defensively: although a JSON
			// parse failure should not echo embedded secret values, a
			// partially-quoted SAS key could in principle appear in the
			// error context.
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
// when the stored config is corrupt - caller logs and proceeds), and a
// fatal error (non-nil when the inbound list itself is unparseable or
// re-marshaling fails). A corrupt stored list is non-fatal because the
// new config replaces it anyway, but the operator must be told because
// secret preservation cannot run.
func mergeConnectionSecretsJSON(inboundRaw, prevRaw string) (merged string, prevParseErr, err error) {
	inboundList, err := parseConnections(inboundRaw)
	if err != nil {
		return "", nil, fmt.Errorf("parse inbound: %w", err)
	}
	prevList, parseErr := parseConnections(prevRaw)
	if parseErr != nil {
		prevParseErr = parseErr
		// continue with an empty prev map; new connections branch handles it
	}

	prevByName := make(map[string]ConnectionConfig, len(prevList))
	for _, c := range prevList {
		prevByName[c.Name] = c
	}

	for i := range inboundList {
		stored, ok := prevByName[inboundList[i].Name]
		if !ok {
			// New connection (or prev was corrupt); sentinel resolves to
			// itself - which clearSentinelSecrets converts to empty so the
			// downstream validator surfaces "required" rather than accepting
			// the sentinel literal as a real secret.
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
	// New connection (no stored counterpart) handled by clearSentinelSecrets.
	clearSentinelSecrets(inbound)
}

// preserveSecret implements the sentinel-merge rule for a single field:
//   - inbound == sentinel -> keep stored (admin reopened the form, did not edit)
//   - inbound != sentinel -> accept inbound (whatever the admin typed)
//
// Note: empty inbound means "the admin cleared this field" - we honor that
// so an admin can actually delete a stored secret (e.g., when transitioning
// from shared-key to service-principal). Because the webapp uses the
// sentinel for "no change" via loadConnectionForEdit, an empty inbound
// here is an explicit clear, not an accident.
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
// than reading p.ctx. Reading p.ctx unsynchronized would be a data race
// (interface read, not atomic), and after a disable/enable cycle p.ctx
// is non-nil but already cancelled, which would make every probe fail
// with "context canceled" instead of running. A save-time probe doesn't
// benefit from plugin-lifetime cancellation anyway: the operation is
// synchronous and short.
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
