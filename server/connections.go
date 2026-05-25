package main

import (
	"context"
	"fmt"
	"time"

	mmModel "github.com/mattermost/mattermost/server/public/model"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
)

const (
	headerPostID   = "X-Post-Id"
	headerConnName = "X-Conn-Name"
	headerFilename = "X-Filename"
	// headerKind discriminates the file payload type carried over the
	// QueueProvider file path. Values: "attachment", "profile_image".
	headerKind = "X-Crossguard-Kind"
	// headerTeamName and headerChanName carry the sender's team and channel
	// names so the inbound side can resolve the local channel via the same
	// rewrite-then-by-name path the message envelope uses.
	headerTeamName = "X-Team-Name"
	headerChanName = "X-Channel-Name"
	// headerFileInfo carries a base64-encoded JSON FileInfo for attachments.
	// Base64 wraps the JSON so newlines and quotes survive every provider's
	// metadata-encoding rules (Azure Blob metadata is HTTP-header-grade,
	// NATS object store uses nats.Header).
	headerFileInfo = "X-File-Info"
	// headerUserID carries the remote user id for profile_image payloads.
	// User ids are globally unique UUIDs so no translation is needed.
	headerUserID = "X-User-Id"

	// kindAttachment and kindProfileImage are the values for headerKind.
	kindAttachment   = "attachment"
	kindProfileImage = "profile_image"
)

// buildTestEnvelope builds a connectivity-check envelope. The caller passes
// the plugin's current epoch so the receiver can correlate the test against
// the sender session it expects to see on subsequent sync_msg envelopes.
// Test envelopes carry Epoch but never Sequence (no channel scope).
// nextOutboundSeq returns the next per-(connName, channelID) sequence
// number under the current epoch. Concurrency-safe; the counter map is
// reset to empty on every OnActivate.
func (p *Plugin) nextOutboundSeq(connName, channelID string) uint64 {
	key := connName + "\x00" + channelID
	p.outboundSeqMu.Lock()
	defer p.outboundSeqMu.Unlock()
	p.outboundSeq[key]++
	return p.outboundSeq[key]
}

// buildTestEnvelope builds a connectivity-check envelope. ConnName is
// also stamped into TeamName and ChannelName so the envelope satisfies
// the constrained wire-schema's SlugType requirement on those fields;
// the receiver ignores team/channel scope on test envelopes
// (inbound.go logs only conn_name and test_id). The format argument
// picks the wire encoding; callers pull it from the outbound
// connection config (ParseWireFormat(conn.MessageFormat)).
func buildTestEnvelope(connName, epoch string, format WireFormat) (*TransportEnvelope, []byte, string, error) {
	msgID := mmModel.NewId()
	env := &TransportEnvelope{
		Version:     1,
		Type:        TransportTypeTest,
		ConnName:    connName,
		Epoch:       epoch,
		TeamName:    connName,
		ChannelName: connName,
		TestID:      msgID,
	}
	data, err := MarshalEnvelope(env, format)
	if err != nil {
		return nil, nil, "", err
	}
	return env, data, msgID, nil
}

func (p *Plugin) connectOutbound() {
	cfg := p.getConfiguration()
	conns, err := cfg.GetOutboundConnections()
	if err != nil {
		p.API.LogError("Failed to parse outbound connections for relay",
			"error_code", errcode.ConnectionsParseOutboundFailed,
			"error", err.Error())
		return
	}

	var pool []outboundConn
	for _, conn := range conns {
		provider, err := p.createProvider(conn, "Outbound")
		if err != nil {
			p.API.LogError("Failed to connect outbound for relay",
				"error_code", errcode.ConnectionsConnectOutboundFailed,
				"name", conn.Name, "provider", conn.Provider, "error", err.Error())
			continue
		}
		pool = append(pool, outboundConn{
			provider:      provider,
			name:          conn.Name,
			healthy:       true,
			lastCheckTime: time.Now(),
		})
		p.API.LogInfo("Outbound connection established for relay",
			"error_code", errcode.ConnectionsOutboundEstablished,
			"name", conn.Name, "provider", conn.Provider)
	}

	p.outboundMu.Lock()
	p.outboundConns = pool
	p.outboundMu.Unlock()
}

func (p *Plugin) closeOutbound() {
	p.outboundMu.Lock()
	conns := p.outboundConns
	p.outboundConns = nil
	p.outboundMu.Unlock()

	for _, oc := range conns {
		_ = oc.provider.Close()
	}
}

func (p *Plugin) reconnectOutbound() {
	p.closeOutbound()
	p.connectOutbound()
}

const healthRecheckInterval = 30 * time.Second

// publishToOutboundConn serializes the TransportEnvelope to XML and publishes
// it to the named outbound connection. Each call publishes exactly one
// envelope; the caller (hooks.go) has already fanned out a SyncMsg into
// one envelope per post plus an optional metadata envelope via
// buildOutboundEnvelopes. Stamps the sender's Epoch on the envelope and a
// fresh per-(connName, channelID) Sequence for sync_msg envelopes.
// Returns an error so the caller can propagate the failure to the
// shared channels server and prevent cursor advance.
func (p *Plugin) publishToOutboundConn(ctx context.Context, env *TransportEnvelope, connName string) error {
	p.outboundMu.RLock()
	var oc *outboundConn
	for i := range p.outboundConns {
		if p.outboundConns[i].name == connName {
			candidate := p.outboundConns[i]
			oc = &candidate
			break
		}
	}
	p.outboundMu.RUnlock()

	if oc == nil {
		return fmt.Errorf("no outbound provider for connection %q", connName)
	}

	if !oc.healthy && time.Since(oc.lastCheckTime) < healthRecheckInterval {
		return fmt.Errorf("outbound connection %q is unhealthy, skipping publish", connName)
	}

	env.Epoch = p.epoch

	// Stamp sequence for sync_msg envelopes (test envelopes carry no
	// channel scope and never get a sequence number). The counter is
	// scoped to the current epoch via an in-memory map that resets on
	// OnActivate, so the first envelope on each channel under a new
	// epoch carries Sequence=1.
	if env.Type == TransportTypeSyncMsg && env.SyncMsg != nil {
		env.Sequence = p.nextOutboundSeq(connName, env.SyncMsg.ChannelId)
	}

	// Pick the wire encoding from the outbound connection's config. A
	// missing config falls back to FormatXML; an unparseable
	// message_format value (which validateConnectionList catches at
	// save time) also falls back to FormatXML rather than failing the
	// publish, since the publish path is hot.
	format := FormatXML
	if cfg, ok := p.outboundConnConfigByName(connName); ok {
		if parsed, err := ParseWireFormat(cfg.MessageFormat); err == nil {
			format = parsed
		}
	}

	data, marshalErr := MarshalEnvelope(env, format)
	if marshalErr != nil {
		p.API.LogError("Failed to serialize outbound envelope",
			"error_code", errcode.ConnectionsSerializePartFailed,
			"name", connName, "error", marshalErr.Error())
		p.updateOutboundHealth(connName, false)
		return marshalErr
	}
	if pubErr := oc.provider.Publish(ctx, data); pubErr != nil {
		p.API.LogError("Failed to publish outbound envelope",
			"error_code", errcode.ConnectionsPublishPartFailed,
			"name", connName, "error", pubErr.Error())
		p.updateOutboundHealth(connName, false)
		return pubErr
	}

	p.updateOutboundHealth(connName, true)
	p.API.LogDebug("Outbound publish completed",
		"connection", connName, "type", env.Type)
	return nil
}

// uploadToOutboundConn uploads file bytes via the named outbound connection's
// QueueProvider file path. Headers are passed through to the provider verbatim.
// Returns an error when the provider is missing or the upload itself fails.
//
// Health tracking is intentionally not shared with publishToOutboundConn: the
// message-publish and file-upload paths are independent on every transport
// (e.g., NATS uses core pub/sub for messages but JetStream Object Store for
// files), so a JetStream Object Store outage must not block message delivery
// and a broken core-NATS connection must not block uploads. Each path manages
// its own retries via the framework's hook-error semantics.
func (p *Plugin) uploadToOutboundConn(ctx context.Context, connName, key string, data []byte, headers map[string]string) error {
	p.outboundMu.RLock()
	var provider QueueProvider
	for i := range p.outboundConns {
		if p.outboundConns[i].name == connName {
			provider = p.outboundConns[i].provider
			break
		}
	}
	p.outboundMu.RUnlock()

	if provider == nil {
		return fmt.Errorf("no outbound provider for connection %q", connName)
	}

	return provider.UploadFile(ctx, key, data, headers)
}

// outboundConnConfigByName returns the outbound connection config for the
// given name, or false if it is not present in the current configuration.
func (p *Plugin) outboundConnConfigByName(connName string) (ConnectionConfig, bool) {
	cfg := p.getConfiguration()
	conns, err := cfg.GetOutboundConnections()
	if err != nil {
		return ConnectionConfig{}, false
	}
	for _, c := range conns {
		if c.Name == connName {
			return c, true
		}
	}
	return ConnectionConfig{}, false
}

// inboundConnConfigByName returns the inbound connection config for the
// given name, or false if it is not present in the current configuration.
func (p *Plugin) inboundConnConfigByName(connName string) (ConnectionConfig, bool) {
	cfg := p.getConfiguration()
	conns, err := cfg.GetInboundConnections()
	if err != nil {
		return ConnectionConfig{}, false
	}
	for _, c := range conns {
		if c.Name == connName {
			return c, true
		}
	}
	return ConnectionConfig{}, false
}

// updateOutboundHealth marks the outbound connection with the given name as
// healthy or unhealthy.
func (p *Plugin) updateOutboundHealth(name string, healthy bool) {
	p.outboundMu.Lock()
	defer p.outboundMu.Unlock()
	for i := range p.outboundConns {
		if p.outboundConns[i].name == name {
			p.outboundConns[i].healthy = healthy
			p.outboundConns[i].lastCheckTime = time.Now()
			return
		}
	}
}

// createProvider constructs a QueueProvider based on the connection config.
func (p *Plugin) createProvider(cfg ConnectionConfig, direction string) (QueueProvider, error) {
	switch cfg.Provider {
	case ProviderNATS, "":
		if cfg.NATS == nil {
			return nil, errMissingNATSConfig
		}
		queueGroup := manifest.Id
		if siteURL := p.API.GetConfig().ServiceSettings.SiteURL; siteURL != nil && *siteURL != "" {
			queueGroup = *siteURL + "/" + manifest.Id
		}
		return newNATSProvider(*cfg.NATS, p.API, direction, queueGroup)
	case ProviderAzureQueue:
		if cfg.AzureQueue == nil {
			return nil, errMissingAzureQueueConfig
		}
		return newAzureProvider(p.ctx, *cfg.AzureQueue, p.API)
	case ProviderAzureBlob:
		if cfg.AzureBlob == nil {
			return nil, errMissingAzureBlobConfig
		}
		isOutbound := direction == "Outbound"
		return newAzureBlobProvider(p.ctx, *cfg.AzureBlob, p.API, &p.client.KV, p.nodeID, cfg.Name, isOutbound)
	case ProviderAzureServiceBus:
		if cfg.AzureServiceBus == nil {
			return nil, errMissingAzureServiceBusConfig
		}
		return newAzureServiceBusProvider(p.ctx, *cfg.AzureServiceBus, p.API)
	default:
		return nil, errUnknownProvider(cfg.Provider)
	}
}
