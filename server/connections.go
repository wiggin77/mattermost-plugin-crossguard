package main

import (
	"context"
	"fmt"
	"time"

	mmModel "github.com/mattermost/mattermost/server/public/model"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/model"
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

const (
	fileSemaphoreSize  = 32
	relaySemaphoreSize = 256

	headerPostID   = "X-Post-Id"
	headerConnName = "X-Conn-Name"
	headerFilename = "X-Filename"
)

func buildTestMessage(format model.Format) ([]byte, string, error) {
	msgID := mmModel.NewId()
	env := &model.Envelope{
		Type:        model.MessageTypeTest,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		TestMessage: &model.TestMessage{ID: msgID},
	}
	data, err := model.Marshal(env, format)
	if err != nil {
		return nil, "", err
	}
	return data, msgID, nil
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
			provider:            provider,
			name:                conn.Name,
			fileTransferEnabled: conn.FileTransferEnabled,
			fileFilterMode:      conn.FileFilterMode,
			fileFilterTypes:     conn.FileFilterTypes,
			messageFormat:       conn.MessageFormat,
			healthy:             true,
			lastCheckTime:       time.Now(),
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

func buildPostEnvelope(msgType string, post *mmModel.Post, channel *mmModel.Channel, teamName, username string) *model.Envelope {
	pm := model.PostMessage{
		PostID:      post.Id,
		RootID:      post.RootId,
		ChannelID:   post.ChannelId,
		ChannelName: channel.Name,
		TeamID:      channel.TeamId,
		TeamName:    teamName,
		UserID:      post.UserId,
		Username:    username,
		MessageText: post.Message,
		CreateAt:    post.CreateAt,
	}
	return &model.Envelope{
		Type:        msgType,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		PostMessage: &pm,
	}
}

func buildDeleteEnvelope(post *mmModel.Post, channel *mmModel.Channel, teamName string) *model.Envelope {
	dm := model.DeleteMessage{
		PostID:      post.Id,
		ChannelID:   post.ChannelId,
		ChannelName: channel.Name,
		TeamID:      channel.TeamId,
		TeamName:    teamName,
	}
	return &model.Envelope{
		Type:          model.MessageTypeDelete,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		DeleteMessage: &dm,
	}
}

func buildReactionEnvelope(msgType string, reaction *mmModel.Reaction, channel *mmModel.Channel, teamName, username string) *model.Envelope {
	rm := model.ReactionMessage{
		PostID:      reaction.PostId,
		ChannelID:   channel.Id,
		ChannelName: channel.Name,
		TeamID:      channel.TeamId,
		TeamName:    teamName,
		UserID:      reaction.UserId,
		Username:    username,
		EmojiName:   reaction.EmojiName,
	}
	return &model.Envelope{
		Type:            msgType,
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		ReactionMessage: &rm,
	}
}

const healthRecheckInterval = 30 * time.Second

func (p *Plugin) publishToOutbound(ctx context.Context, env *model.Envelope, conns []store.TeamConnection) {
	p.outboundMu.RLock()
	pool := make([]outboundConn, len(p.outboundConns))
	copy(pool, p.outboundConns)
	p.outboundMu.RUnlock()

	if len(pool) == 0 {
		return
	}

	for _, oc := range pool {
		if !isOutboundLinked(oc.name, conns) {
			continue
		}

		// Skip unhealthy connections unless it is time for a recheck.
		if !oc.healthy && time.Since(oc.lastCheckTime) < healthRecheckInterval {
			continue
		}

		format := model.Format(oc.messageFormat)
		if format == "" {
			format = model.FormatJSON
		}
		data, err := model.Marshal(env, format)
		if err != nil {
			p.API.LogError("Failed to serialize outbound message",
				"error_code", errcode.ConnectionsSerializeFailed,
				"name", oc.name, "format", string(format), "error", err.Error())
			continue
		}

		// Check message size limit and split if needed.
		maxSize := oc.provider.MaxMessageSize()
		if maxSize > 0 && len(data) > maxSize && env.PostMessage != nil {
			parts := splitMessage(env, format, maxSize)

			if len(parts) > 1 {
				p.API.LogInfo("Message split into parts for provider size limit",
					"error_code", errcode.ConnectionsMessageSplit,
					"connection", oc.name, "parts", len(parts), "originalSize", len(data))
			}

			// Determine the root ID for continuation parts.
			rootID := env.PostMessage.RootID
			if rootID == "" {
				rootID = env.PostMessage.PostID
			}

			partFailed := false
			for partIdx, partText := range parts {
				// Build a per-part envelope without mutating the original.
				partEnv := *env
				pm := *env.PostMessage
				pm.MessageText = partText
				if partIdx > 0 {
					pm.PostID = fmt.Sprintf("%s_part%d", env.PostMessage.PostID, partIdx+1)
					pm.RootID = rootID
				}
				partEnv.PostMessage = &pm

				partData, marshalErr := model.Marshal(&partEnv, format)
				if marshalErr != nil {
					p.API.LogError("Failed to serialize message part",
						"error_code", errcode.ConnectionsSerializePartFailed,
						"name", oc.name, "part", partIdx+1, "error", marshalErr.Error())
					partFailed = true
					break
				}

				if pubErr := oc.provider.Publish(ctx, partData); pubErr != nil {
					p.API.LogError("Failed to publish message part",
						"error_code", errcode.ConnectionsPublishPartFailed,
						"name", oc.name, "part", partIdx+1, "error", pubErr.Error())
					partFailed = true
					break
				}
			}

			p.updateOutboundHealth(oc.name, !partFailed)
			continue
		}

		if err := oc.provider.Publish(ctx, data); err != nil {
			p.API.LogError("Failed to publish to outbound after retries",
				"error_code", errcode.ConnectionsPublishAfterRetriesFail,
				"name", oc.name, "error", err.Error())
			p.updateOutboundHealth(oc.name, false)
		} else {
			p.updateOutboundHealth(oc.name, true)
		}
	}
}

// updateOutboundHealth marks the outbound connection with the given name as
// healthy or unhealthy. Looking up by name (rather than by slice index from a
// snapshot) keeps the health update pointing at the right connection even if
// outboundConns has been reloaded in the meantime.
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

// isOutboundLinked checks if an outbound connection name is in the team's linked list.
func isOutboundLinked(outboundName string, conns []store.TeamConnection) bool {
	for _, tc := range conns {
		if tc.Direction == "outbound" && tc.Connection == outboundName {
			return true
		}
	}
	return false
}

func (p *Plugin) uploadPostFiles(post *mmModel.Post, conns []store.TeamConnection) {
	p.outboundMu.RLock()
	var fileConns []outboundConn
	for _, oc := range p.outboundConns {
		if oc.fileTransferEnabled && isOutboundLinked(oc.name, conns) {
			fileConns = append(fileConns, oc)
		}
	}
	p.outboundMu.RUnlock()

	if len(fileConns) == 0 {
		return
	}

	var maxFileSize int64 = defaultMaxFileSize
	if cfg := p.API.GetConfig(); cfg != nil && cfg.FileSettings.MaxFileSize != nil {
		maxFileSize = *cfg.FileSettings.MaxFileSize
	}

	for _, fileID := range post.FileIds {
		fi, appErr := p.API.GetFileInfo(fileID)
		if appErr != nil {
			p.API.LogError("Failed to get file info for relay",
				"error_code", errcode.ConnectionsGetFileInfoFailed,
				"file_id", fileID, "post_id", post.Id, "error", appErr.Error())
			continue
		}
		if fi.Size > maxFileSize {
			p.API.LogWarn("Skipping oversized file for relay",
				"error_code", errcode.ConnectionsSkipOversizedFile,
				"file", fi.Name, "size", fi.Size, "max", maxFileSize)
			continue
		}

		// First pass: azure-blob providers defer the upload via QueueFileRef.
		// Determine whether any non-blob providers still need the file bytes.
		needsDownload := false
		for _, oc := range fileConns {
			if !isFileAllowed(fi.Name, oc.fileFilterMode, oc.fileFilterTypes) {
				continue
			}
			if blobProvider, ok := oc.provider.(*azureBlobProvider); ok {
				blobProvider.QueueFileRef(post.Id, fi.Id, fi.Name)
				continue
			}
			needsDownload = true
		}

		if !needsDownload {
			continue
		}

		fileData, appErr := p.API.GetFile(fi.Id)
		if appErr != nil {
			p.API.LogError("Failed to download file for relay",
				"error_code", errcode.ConnectionsDownloadFileFailed,
				"file_id", fi.Id, "file", fi.Name, "error", appErr.Error())
			continue
		}

		for _, oc := range fileConns {
			if _, ok := oc.provider.(*azureBlobProvider); ok {
				continue
			}
			if !isFileAllowed(fi.Name, oc.fileFilterMode, oc.fileFilterTypes) {
				p.API.LogInfo("Outbound file filtered by policy",
					"error_code", errcode.ConnectionsOutboundFileFiltered,
					"file", fi.Name, "conn", oc.name)
				continue
			}

			p.wg.Add(1)
			go func(oc outboundConn, fi *mmModel.FileInfo, data []byte) {
				defer p.wg.Done()

				select {
				case p.fileSem <- struct{}{}:
					defer func() { <-p.fileSem }()
				default:
					p.API.LogWarn("File semaphore full, skipping file upload",
						"error_code", errcode.ConnectionsFileSemaphoreFull,
						"file", fi.Name, "conn", oc.name)
					return
				}

				key := post.Id + "/" + mmModel.NewId()
				headers := map[string]string{
					headerPostID:   post.Id,
					headerConnName: oc.name,
					headerFilename: fi.Name,
				}

				if err := oc.provider.UploadFile(p.ctx, key, data, headers); err != nil {
					p.API.LogError("Failed to upload file",
						"error_code", errcode.ConnectionsUploadFileFailed,
						"key", key, "conn", oc.name, "error", err.Error())
				}
			}(oc, fi, fileData)
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
		return newNATSProvider(*cfg.NATS, p.API, direction)
	case ProviderAzureQueue:
		if cfg.AzureQueue == nil {
			return nil, errMissingAzureQueueConfig
		}
		return newAzureProvider(p.ctx, *cfg.AzureQueue, p.API)
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
		return newAzureServiceBusProvider(p.ctx, *cfg.AzureServiceBus, p.API)
	default:
		return nil, errUnknownProvider(cfg.Provider)
	}
}

const safetyMargin = 500

// splitMessage splits the message text into parts that each fit within the
// provider's size limit. Returns a single-element slice (with no label) if the
// message already fits. Otherwise returns N parts each prefixed with
// "[Part X/N] ".
func splitMessage(env *model.Envelope, format model.Format, maxSize int) []string {
	originalText := env.PostMessage.MessageText

	// Measure envelope overhead with empty text.
	env.PostMessage.MessageText = ""
	overhead, err := model.Marshal(env, format)
	env.PostMessage.MessageText = originalText
	if err != nil {
		return []string{originalText}
	}

	available := maxSize - len(overhead) - safetyMargin
	if available <= 0 {
		available = 1
	}

	// If text fits, return as-is (no label).
	if len(originalText) <= available {
		return []string{originalText}
	}

	// First pass: estimate part count to know label size.
	// Label format: "[Part NNNN/MMMM] " is at most 25 bytes.
	const maxLabelLen = 25
	textAvailable := available - maxLabelLen
	if textAvailable <= 0 {
		textAvailable = 1
	}

	// Split text into chunks at UTF-8 safe boundaries.
	var rawChunks []string
	remaining := originalText
	for len(remaining) > 0 {
		chunkSize := min(textAvailable, len(remaining))
		chunk := remaining[:chunkSize]
		// Ensure UTF-8 safe boundary.
		for len(chunk) > 0 && chunk[len(chunk)-1]&0xC0 == 0x80 {
			chunk = chunk[:len(chunk)-1]
		}
		if len(chunk) > 0 && chunk[len(chunk)-1]&0x80 != 0 {
			chunk = chunk[:len(chunk)-1]
		}
		if len(chunk) == 0 {
			// Safety valve: advance by one byte to avoid infinite loop.
			chunk = remaining[:1]
		}
		rawChunks = append(rawChunks, chunk)
		remaining = remaining[len(chunk):]
	}

	// Second pass: add labels now that we know total count.
	total := len(rawChunks)
	parts := make([]string, total)
	for i, chunk := range rawChunks {
		parts[i] = fmt.Sprintf("[Part %d/%d] %s", i+1, total, chunk)
	}

	return parts
}
