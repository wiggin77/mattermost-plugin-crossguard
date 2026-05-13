package main

import (
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
)

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
type AzureQueueProviderConfig struct {
	QueueServiceURL         string `json:"queue_service_url"`
	BlobServiceURL          string `json:"blob_service_url"`
	AccountName             string `json:"account_name"`
	AccountKey              string `json:"account_key"`
	QueueName               string `json:"queue_name"`
	BlobContainerName       string `json:"blob_container_name"`
	PollIntervalSeconds     int    `json:"poll_interval_seconds,omitempty"`      // default 5
	BlobPollIntervalSeconds int    `json:"blob_poll_interval_seconds,omitempty"` // default 15
}

// AzureServiceBusProviderConfig holds Azure Service Bus queue + optional Azure Blob file transfer settings.
//
// Auth is connection-string only in Phase 1. Managed Identity is tracked as a
// separate cross-provider effort. Message lock duration is NOT a client knob
// (it is an Azure entity property set on the queue definition), so there is
// no corresponding field here.
type AzureServiceBusProviderConfig struct {
	ConnectionString string `json:"connection_string"`
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
}

// AzureBlobProviderConfig holds Azure Blob Storage provider settings for batch message relay.
type AzureBlobProviderConfig struct {
	ServiceURL               string `json:"service_url"`
	AccountName              string `json:"account_name"`
	AccountKey               string `json:"account_key"`
	BlobContainerName        string `json:"blob_container_name"`
	FlushIntervalSeconds     int    `json:"flush_interval_seconds,omitempty"`      // default 60
	BlobLockMaxAgeSeconds    int    `json:"blob_lock_max_age_seconds,omitempty"`   // default 300 (5 min)
	BatchPollIntervalSeconds int    `json:"batch_poll_interval_seconds,omitempty"` // default 30
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

	if strings.TrimSpace(az.AccountName) == "" {
		errs = append(errs, fmt.Sprintf("%s: account_name is required", prefix))
	}

	if strings.TrimSpace(az.AccountKey) == "" {
		errs = append(errs, fmt.Sprintf("%s: account_key is required", prefix))
	}

	if strings.TrimSpace(az.QueueName) == "" {
		errs = append(errs, fmt.Sprintf("%s: queue_name is required", prefix))
	}

	if conn.FileTransferEnabled && strings.TrimSpace(az.BlobContainerName) == "" {
		errs = append(errs, fmt.Sprintf("%s: blob_container_name is required when file_transfer_enabled is true", prefix))
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

	if strings.TrimSpace(ab.AccountName) == "" {
		errs = append(errs, fmt.Sprintf("%s: account_name is required", prefix))
	}

	if strings.TrimSpace(ab.AccountKey) == "" {
		errs = append(errs, fmt.Sprintf("%s: account_key is required", prefix))
	}

	if strings.TrimSpace(ab.BlobContainerName) == "" {
		errs = append(errs, fmt.Sprintf("%s: blob_container_name is required", prefix))
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

	if strings.TrimSpace(sb.ConnectionString) == "" {
		errs = append(errs, fmt.Sprintf("%s: connection_string is required", prefix))
	}

	trimmedQueue := strings.TrimSpace(sb.QueueName)
	switch {
	case trimmedQueue == "":
		errs = append(errs, fmt.Sprintf("%s: queue_name is required", prefix))
	case len(trimmedQueue) > serviceBusQueueNameMaxLen:
		errs = append(errs, fmt.Sprintf("%s: queue_name must be at most %d characters", prefix, serviceBusQueueNameMaxLen))
	case !serviceBusQueueNamePattern.MatchString(trimmedQueue):
		errs = append(errs, fmt.Sprintf("%s: queue_name may contain only letters, digits, periods, hyphens, underscores, and forward slashes", prefix))
	}

	if conn.FileTransferEnabled {
		if strings.TrimSpace(sb.BlobServiceURL) == "" {
			errs = append(errs, fmt.Sprintf("%s: blob_service_url is required when file_transfer_enabled is true", prefix))
		} else if _, err := url.Parse(sb.BlobServiceURL); err != nil {
			errs = append(errs, fmt.Sprintf("%s: blob_service_url is not a valid URL: %v", prefix, err))
		}
		if strings.TrimSpace(sb.BlobAccountName) == "" {
			errs = append(errs, fmt.Sprintf("%s: blob_account_name is required when file_transfer_enabled is true", prefix))
		}
		if strings.TrimSpace(sb.BlobAccountKey) == "" {
			errs = append(errs, fmt.Sprintf("%s: blob_account_key is required when file_transfer_enabled is true", prefix))
		}
		if strings.TrimSpace(sb.BlobContainerName) == "" {
			errs = append(errs, fmt.Sprintf("%s: blob_container_name is required when file_transfer_enabled is true", prefix))
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
// test-connection probes.
func isTestMessage(data []byte) (string, bool) {
	env, err := UnmarshalEnvelope(data)
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

	if err := cfg.validate(); err != nil {
		p.API.LogWarn("Plugin configuration has validation warnings",
			"error_code", errcode.ConfigValidationWarn,
			"error", err.Error())
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
