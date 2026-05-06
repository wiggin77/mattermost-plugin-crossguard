package main

import (
	"context"
	"fmt"
	"time"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
)

const (
	relaySemaphoreSize = 256

	headerPostID   = "X-Post-Id"
	headerConnName = "X-Conn-Name"
	headerFilename = "X-Filename"
)

func buildTestEnvelope() (*TransportEnvelope, []byte, string, error) {
	msgID := newID()
	env := &TransportEnvelope{
		Version: 1,
		Type:    TransportTypeTest,
		TestID:  msgID,
	}
	data, err := MarshalEnvelope(env)
	if err != nil {
		return nil, nil, "", err
	}
	return env, data, msgID, nil
}

// newID generates a small random ID for test messages without depending on
// the model package's NewId.
func newID() string {
	return fmt.Sprintf("test-%d", time.Now().UnixNano())
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
// it to the named outbound connection. Splits the SyncMsg if the resulting
// payload exceeds the provider's MaxMessageSize. Returns an error if any
// part fails to publish (so the caller can propagate the failure to the
// shared channels server, preventing cursor advance).
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

	maxSize := oc.provider.MaxMessageSize()
	parts, err := splitTransportEnvelope(env, maxSize)
	if err != nil {
		p.updateOutboundHealth(connName, false)
		return fmt.Errorf("split envelope: %w", err)
	}

	if len(parts) > 1 {
		p.API.LogInfo("Envelope split into parts for provider size limit",
			"error_code", errcode.ConnectionsMessageSplit,
			"connection", connName, "parts", len(parts))
	}

	for i, part := range parts {
		data, marshalErr := MarshalEnvelope(part)
		if marshalErr != nil {
			p.API.LogError("Failed to serialize outbound envelope part",
				"error_code", errcode.ConnectionsSerializePartFailed,
				"name", connName, "part", i+1, "error", marshalErr.Error())
			p.updateOutboundHealth(connName, false)
			return marshalErr
		}
		if pubErr := oc.provider.Publish(ctx, data); pubErr != nil {
			p.API.LogError("Failed to publish outbound envelope part",
				"error_code", errcode.ConnectionsPublishPartFailed,
				"name", connName, "part", i+1, "error", pubErr.Error())
			p.updateOutboundHealth(connName, false)
			return pubErr
		}
	}

	p.updateOutboundHealth(connName, true)
	p.API.LogDebug("Outbound publish completed",
		"connection", connName, "type", env.Type, "parts", len(parts))
	return nil
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
		return newAzureProvider(*cfg.AzureQueue, p.API)
	case ProviderAzureBlob:
		if cfg.AzureBlob == nil {
			return nil, errMissingAzureBlobConfig
		}
		getFile := func(fileID string) ([]byte, error) {
			data, appErr := p.API.GetFile(fileID)
			if appErr != nil {
				return nil, appErr
			}
			return data, nil
		}
		isOutbound := direction == "Outbound"
		return newAzureBlobProvider(p.ctx, *cfg.AzureBlob, p.API, &p.client.KV, p.nodeID, cfg.Name, getFile, isOutbound)
	case ProviderAzureServiceBus:
		if cfg.AzureServiceBus == nil {
			return nil, errMissingAzureServiceBusConfig
		}
		return newAzureServiceBusProvider(*cfg.AzureServiceBus, p.API)
	default:
		return nil, errUnknownProvider(cfg.Provider)
	}
}
