package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

// apiError represents a structured error with an HTTP status code.
type apiError struct {
	Message string
	Status  int
}

func (e *apiError) Error() string {
	return e.Message
}

// TeamStatusResponse is the JSON response for a single team's initialization status.
type TeamStatusResponse struct {
	TeamID            string                 `json:"team_id"`
	TeamName          string                 `json:"team_name"`
	TeamDisplayName   string                 `json:"team_display_name"`
	Initialized       bool                   `json:"initialized"`
	LinkedConnections []store.TeamConnection `json:"linked_connections"`
	Connections       []ConnectionStatus     `json:"connections"`
	RequestMode       bool                   `json:"request_mode,omitempty"`
}

// TeamStatusEntry represents one initialized team in the global status response.
type TeamStatusEntry struct {
	TeamID            string                 `json:"team_id"`
	TeamName          string                 `json:"team_name"`
	DisplayName       string                 `json:"display_name"`
	LinkedConnections []store.TeamConnection `json:"linked_connections"`
}

// RedactedConnection exposes only safe fields from a connection config.
type RedactedConnection struct {
	Name                string `json:"name"`
	Direction           string `json:"direction"`
	Provider            string `json:"provider"`
	Address             string `json:"address,omitempty"`
	AuthType            string `json:"auth_type,omitempty"`
	Subject             string `json:"subject,omitempty"`
	FileTransferEnabled bool   `json:"file_transfer_enabled"`
	FileFilterMode      string `json:"file_filter_mode,omitempty"`
	FileFilterTypes     string `json:"file_filter_types,omitempty"`
	// MessageFormat is the resolved per-connection wire format ("xml"
	// or "json" for outbound; "auto" for inbound, which auto-detects).
	// Surfaced in the slash-command status table so operators can see
	// the configured wire encoding at a glance without inspecting the
	// raw connection config.
	MessageFormat     string `json:"message_format,omitempty"`
	QueueName         string `json:"queue_name,omitempty"`
	BlobContainerName string `json:"blob_container_name,omitempty"`

	// Service Principal-related fields. These are operational identifiers
	// (which AAD identity is in use, which cloud, which namespace), NOT
	// secrets, so they are safe to expose to admin status views. The
	// corresponding secret fields (ClientSecret, AccountKey,
	// ConnectionString, BlobAccountKey) MUST never appear here.
	AuthMode            string `json:"auth_mode,omitempty"`
	AzureCloud          string `json:"azure_cloud,omitempty"`
	TenantID            string `json:"tenant_id,omitempty"`
	ClientID            string `json:"client_id,omitempty"`
	ServiceBusNamespace string `json:"service_bus_namespace,omitempty"`
}

// GlobalStatusResponse is the JSON response for the system-wide status endpoint.
type GlobalStatusResponse struct {
	Teams       []TeamStatusEntry    `json:"teams"`
	Connections []RedactedConnection `json:"connections"`
	Warnings    []string             `json:"warnings,omitempty"`
}

const (
	crossguardHeaderPrefix = "\U0001F517 " // link emoji + space
)

// connKey returns a display key for a TeamConnection (e.g. "outbound:my-conn").
func connKey(tc store.TeamConnection) string {
	return tc.Direction + ":" + tc.Connection
}

// connectionDisplayNames formats a slice of TeamConnection as display strings.
func connectionDisplayNames(conns []store.TeamConnection) []string {
	names := make([]string, len(conns))
	for i, tc := range conns {
		names[i] = connKey(tc)
	}
	return names
}

// teamTownSquareLink returns a Markdown link to a team's Town Square channel.
func teamTownSquareLink(team *model.Team) string {
	return fmt.Sprintf("[**%s**](/%s/channels/town-square)", team.DisplayName, team.Name)
}

// connDetails holds resolved display properties for a connection key.
type connDetails struct {
	direction string
	provider  string
	msgFmt    string
	files     string
}

// resolveConnDetails extracts display properties for a connection key from the
// connection config map, applying defaults for missing values.
func resolveConnDetails(ck string, connMap map[string]ConnectionConfig) connDetails {
	d := connDetails{
		direction: strings.SplitN(ck, ":", 2)[0],
	}
	if cc, ok := connMap[ck]; ok {
		d.provider = cc.Provider
		d.files = "Disabled"
		if cc.FileTransferEnabled {
			d.files = "Enabled"
			if cc.FileFilterMode != "" && cc.FileFilterTypes != "" {
				d.files += fmt.Sprintf(" (%s: %s)", cc.FileFilterMode, cc.FileFilterTypes)
			}
		}
	}
	if d.provider == "" {
		d.provider = "nats"
	}
	d.msgFmt = "xml"
	return d
}

// writeConnDetailRows appends the standard connection detail table rows to a
// strings.Builder.
func writeConnDetailRows(sb *strings.Builder, teamLink, ck string, d connDetails) {
	fmt.Fprintf(sb, "| **Team** | %s |\n", teamLink)
	fmt.Fprintf(sb, "| **Connection** | `%s` |\n", ck)
	fmt.Fprintf(sb, "| **Direction** | %s |\n", d.direction)
	fmt.Fprintf(sb, "| **Provider** | %s |\n", d.provider)
	fmt.Fprintf(sb, "| **Message format** | %s |\n", d.msgFmt)
	if d.files != "" {
		fmt.Fprintf(sb, "| **File transfer** | %s |\n", d.files)
	}
}

// channelLink returns a Markdown link to a channel.
func channelLink(channel *model.Channel, team *model.Team) string {
	return fmt.Sprintf("[**#%s**](/%s/channels/%s)", channel.DisplayName, team.Name, channel.Name)
}

// buildRequestDMMessage builds the full Markdown message for a connection link
// request DM sent to admins. It consolidates all request details into a single
// table. Pass channelLink="" for team-level requests.
func buildRequestDMMessage(userLabel, teamLink, ck string, connMap map[string]ConnectionConfig, chanLink string) string {
	d := resolveConnDetails(ck, connMap)

	header := "#### :link: Connection Link Request\n\n"
	if chanLink != "" {
		header = "#### :link: Channel Connection Link Request\n\n"
	}

	var sb strings.Builder
	sb.WriteString(header)
	sb.WriteString("| Property | Details |\n|:--|:--|\n")
	fmt.Fprintf(&sb, "| **Requested by** | %s |\n", userLabel)
	writeConnDetailRows(&sb, teamLink, ck, d)
	if chanLink != "" {
		fmt.Fprintf(&sb, "| **Channel** | %s |\n", chanLink)
	}

	return sb.String()
}

// buildRequestConfirmationMessage builds a Markdown message sent to the
// requester confirming their link request has been submitted. Pass
// channelLink="" and approverLabel="system admins" for team-level requests.
func buildRequestConfirmationMessage(teamLink, ck string, connMap map[string]ConnectionConfig, chanLink, approverLabel string) string {
	d := resolveConnDetails(ck, connMap)

	var sb strings.Builder
	sb.WriteString("#### :link: Connection Link Request Submitted\n\n")
	fmt.Fprintf(&sb, "Your request has been sent to %s for approval.\n\n", approverLabel)
	sb.WriteString("| Property | Details |\n|:--|:--|\n")
	writeConnDetailRows(&sb, teamLink, ck, d)
	if chanLink != "" {
		fmt.Fprintf(&sb, "| **Channel** | %s |\n", chanLink)
	}
	return sb.String()
}

func addCrossguardHeaderPrefix(header string) string {
	if strings.HasPrefix(header, crossguardHeaderPrefix) {
		return header
	}
	return crossguardHeaderPrefix + header
}

func removeCrossguardHeaderPrefix(header string) string {
	return strings.TrimPrefix(header, crossguardHeaderPrefix)
}

func (p *Plugin) publishChannelConnectionUpdate(channelID string, connections []store.TeamConnection) {
	keys := make([]string, len(connections))
	for i, tc := range connections {
		keys[i] = connKey(tc)
	}
	p.API.PublishWebSocketEvent("channel_connections_updated", map[string]any{
		"channel_id":  channelID,
		"connections": strings.Join(keys, ","),
	}, &model.WebsocketBroadcast{ChannelId: channelID})
}

// initTeamForCrossGuard links a connection to a team. If the team was not
// previously initialized, it also adds it to the initialized teams list and
// posts an announcement. Returns (team, alreadyLinked, error).
func (p *Plugin) initTeamForCrossGuard(user *model.User, teamID string, conn store.TeamConnection) (*model.Team, bool, *apiError) {
	team, appErr := p.API.GetTeam(teamID)
	if appErr != nil {
		return nil, false, &apiError{Message: "team not found", Status: 404}
	}

	existing, err := p.kvstore.GetTeamConnections(teamID)
	if err != nil {
		p.API.LogError("Failed to get team connections",
			"error_code", errcode.ServiceInitTeamGetConnsFailed,
			"team_id", teamID, "error", err.Error())
		return nil, false, &apiError{Message: "failed to check team initialization state", Status: 500}
	}

	for _, tc := range existing {
		if tc.Matches(conn) {
			return team, true, nil
		}
	}

	if addErr := p.kvstore.AddTeamConnection(teamID, conn); addErr != nil {
		p.API.LogError("Failed to add team connection",
			"error_code", errcode.ServiceAddTeamConnFailed,
			"team_id", teamID, "conn", connKey(conn), "error", addErr.Error())
		return nil, false, &apiError{Message: "failed to save team initialization state", Status: 500}
	}

	updated, err := p.kvstore.GetTeamConnections(teamID)
	if err != nil {
		p.API.LogError("Failed to re-read team connections",
			"error_code", errcode.ServiceInitTeamReReadConnsFailed,
			"team_id", teamID, "error", err.Error())
		return nil, false, &apiError{Message: "failed to save team initialization state", Status: 500}
	}

	if len(updated) == 1 {
		if err := p.kvstore.AddInitializedTeamID(teamID); err != nil {
			p.API.LogError("Failed to add team to initialized list",
				"error_code", errcode.ServiceAddTeamInitializedFailed,
				"team_id", teamID, "error", err.Error())
			return nil, false, &apiError{Message: "failed to save team initialization state", Status: 500}
		}
	}

	channel, appErr := p.API.GetChannelByName(teamID, model.DefaultChannelName, false)
	if appErr == nil {
		displayName := connKey(conn)
		post := &model.Post{
			UserId:    p.botUserID,
			ChannelId: channel.Id,
			Message:   fmt.Sprintf("Cross Guard connection `%s` linked to this team by @%s. (team ID: %s, team name: %s)", displayName, user.Username, team.Id, team.Name),
		}
		if _, appErr := p.API.CreatePost(post); appErr != nil {
			p.API.LogWarn("Failed to post initialization message",
				"error_code", errcode.ServicePostTeamInitMsgFailed,
				"error", appErr.Error())
		}
	}

	return team, false, nil
}

// getTeamStatus returns the initialization status and linked connections for a team.
// If callingUser is non-nil, RequestMode and RequestPending fields are populated.
func (p *Plugin) getTeamStatus(teamID string, callingUser *model.User) (*TeamStatusResponse, *apiError) {
	team, appErr := p.API.GetTeam(teamID)
	if appErr != nil {
		return nil, &apiError{Message: "team not found", Status: 404}
	}

	conns, err := p.kvstore.GetTeamConnections(teamID)
	if err != nil {
		p.API.LogError("Failed to check team status",
			"error_code", errcode.ServiceCheckTeamStatusFailed,
			"team_id", teamID, "error", err.Error())
		return nil, &apiError{Message: "failed to check team status", Status: 500}
	}

	allConns := p.getAllConnectionNames()
	connMap := p.getConnectionMap()
	configSet := make(map[string]store.TeamConnection, len(allConns))
	connSet := make(map[string]store.TeamConnection)
	for _, tc := range allConns {
		key := connKey(tc)
		configSet[key] = tc
		connSet[key] = tc
	}
	for _, tc := range conns {
		key := connKey(tc)
		connSet[key] = tc
	}

	relevantKeys := make([]string, 0, len(connSet))
	for key := range connSet {
		relevantKeys = append(relevantKeys, key)
	}
	sort.Strings(relevantKeys)

	linkedSet := make(map[string]struct{}, len(conns))
	for _, tc := range conns {
		linkedSet[connKey(tc)] = struct{}{}
	}

	cfg := p.getConfiguration()
	checkPending := cfg.isRequestMode()

	statuses := make([]ConnectionStatus, 0, len(relevantKeys))
	for _, key := range relevantKeys {
		tc := connSet[key]
		_, inConfig := configSet[key]
		_, isLinked := linkedSet[key]
		cc := connMap[key]
		cs := ConnectionStatus{
			Name:                tc.Connection,
			Direction:           tc.Direction,
			Provider:            cc.Provider,
			Linked:              isLinked,
			Orphaned:            !inConfig,
			RemoteTeamName:      tc.RemoteTeamName,
			FileTransferEnabled: cc.FileTransferEnabled,
			FileFilterMode:      cc.FileFilterMode,
			FileFilterTypes:     cc.FileFilterTypes,
			MessageFormat:       statusMessageFormat(tc.Direction, cc.MessageFormat),
		}
		if checkPending && !isLinked {
			req, reqErr := p.kvstore.GetConnectionRequest(teamID, key)
			if reqErr != nil {
				p.API.LogWarn("Failed to check pending connection request",
					"error_code", errcode.RequestGetFailed,
					"team_id", teamID, "conn_key", key, "error", reqErr.Error())
			} else if req != nil {
				cs.RequestPending = true
			}
		}
		statuses = append(statuses, cs)
	}

	resp := &TeamStatusResponse{
		TeamID:            team.Id,
		TeamName:          team.Name,
		TeamDisplayName:   team.DisplayName,
		Initialized:       len(conns) > 0,
		LinkedConnections: conns,
		Connections:       statuses,
	}

	if callingUser != nil {
		resp.RequestMode = cfg.isRequestMode() && !callingUser.IsSystemAdmin()
	}

	return resp, nil
}

// getGlobalStatus returns the status of all initialized teams and redacted connections.
func (p *Plugin) getGlobalStatus() (*GlobalStatusResponse, *apiError) {
	teamIDs, err := p.kvstore.GetInitializedTeamIDs()
	if err != nil {
		p.API.LogError("Failed to get initialized teams",
			"error_code", errcode.ServiceGetInitializedTeamsFailed,
			"error", err.Error())
		return nil, &apiError{Message: "failed to get initialized teams", Status: 500}
	}

	teams := make([]TeamStatusEntry, 0, len(teamIDs))
	for _, teamID := range teamIDs {
		team, appErr := p.API.GetTeam(teamID)
		if appErr != nil {
			p.API.LogWarn("Failed to look up team for status",
				"error_code", errcode.ServiceTeamStatusLookupTeamFailed,
				"team_id", teamID, "error", appErr.Error())
			teams = append(teams, TeamStatusEntry{TeamID: teamID, DisplayName: "(unknown)", TeamName: "(error)"})
			continue
		}

		conns, connErr := p.kvstore.GetTeamConnections(teamID)
		if connErr != nil {
			p.API.LogWarn("Failed to get team connections for status",
				"error_code", errcode.ServiceTeamStatusGetConnsFailed,
				"team_id", teamID, "error", connErr.Error())
		}

		teams = append(teams, TeamStatusEntry{
			TeamID:            team.Id,
			TeamName:          team.Name,
			DisplayName:       team.DisplayName,
			LinkedConnections: conns,
		})
	}

	cfg := p.getConfiguration()
	outbound, outErr := cfg.GetOutboundConnections()
	inbound, inErr := cfg.GetInboundConnections()

	connections := redactConnections(outbound, inbound)

	resp := &GlobalStatusResponse{
		Teams:       teams,
		Connections: connections,
	}

	if outErr != nil {
		p.API.LogWarn("Failed to parse outbound connection configuration",
			"error_code", errcode.ServiceParseOutboundConnFailed,
			"error", outErr.Error())
		resp.Warnings = append(resp.Warnings, "Failed to parse outbound connection configuration")
	}
	if inErr != nil {
		p.API.LogWarn("Failed to parse inbound connection configuration",
			"error_code", errcode.ServiceParseInboundConnFailed,
			"error", inErr.Error())
		resp.Warnings = append(resp.Warnings, "Failed to parse inbound connection configuration")
	}

	return resp, nil
}

// ChannelStatusResponse is the JSON response for a channel's connection status.
type ChannelStatusResponse struct {
	ChannelID          string             `json:"channel_id"`
	ChannelName        string             `json:"channel_name"`
	ChannelDisplayName string             `json:"channel_display_name"`
	TeamName           string             `json:"team_name"`
	TeamConnections    []ConnectionStatus `json:"team_connections"`
	ChannelRequestMode bool               `json:"channel_request_mode,omitempty"`
}

// ConnectionStatus represents a single connection and whether it is linked.
type ConnectionStatus struct {
	Name                string `json:"name"`
	Direction           string `json:"direction"`
	Provider            string `json:"provider,omitempty"`
	Linked              bool   `json:"linked"`
	Orphaned            bool   `json:"orphaned,omitempty"`
	RemoteTeamName      string `json:"remote_team_name,omitempty"`
	FileTransferEnabled bool   `json:"file_transfer_enabled"`
	FileFilterMode      string `json:"file_filter_mode,omitempty"`
	FileFilterTypes     string `json:"file_filter_types,omitempty"`
	// MessageFormat surfaces the outbound wire encoding (xml or json)
	// in the slash-command status table. Empty for inbound rows since
	// inbound auto-detects format on a per-envelope basis.
	MessageFormat  string `json:"message_format,omitempty"`
	RequestPending bool   `json:"request_pending,omitempty"`
}

// statusMessageFormat resolves a ConnectionConfig.MessageFormat value
// to the label shown in /crossguard status. Outbound connections
// report the parsed wire format ("xml" or "json"); inbound connections
// report "auto" because the inbound side auto-detects format from the
// envelope bytes rather than from configuration. The defaulting is
// the same as ParseWireFormat: empty maps to "xml".
func statusMessageFormat(direction, configured string) string {
	if direction == "inbound" {
		return "auto"
	}
	format, err := ParseWireFormat(configured)
	if err != nil {
		// Invalid value caught by validateConnectionList at save time;
		// fall back to the default rather than dragging the status
		// table into an error path.
		return FormatXML.String()
	}
	return format.String()
}

// getChannelStatus returns the connection status for a channel, showing
// team-linked connections and any orphaned channel connections.
// If callingUser is non-nil, ChannelRequestMode and RequestPending fields are populated.
func (p *Plugin) getChannelStatus(channelID string, callingUser *model.User) (*ChannelStatusResponse, *apiError) {
	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		return nil, &apiError{Message: "channel not found", Status: 404}
	}

	if channel.Type == model.ChannelTypeDirect || channel.Type == model.ChannelTypeGroup {
		return nil, &apiError{Message: "Cross Guard is not available for direct or group messages", Status: 400}
	}

	team, appErr := p.API.GetTeam(channel.TeamId)
	if appErr != nil {
		return nil, &apiError{Message: "team not found", Status: 404}
	}

	channelConns, err := p.kvstore.GetChannelConnections(channelID)
	if err != nil {
		p.API.LogError("Failed to get channel connections",
			"error_code", errcode.ServiceTeardownGetChanConnsFailed,
			"channel_id", channelID, "error", err.Error())
		return nil, &apiError{Message: "failed to get channel connections", Status: 500}
	}

	teamConns, err := p.kvstore.GetTeamConnections(channel.TeamId)
	if err != nil {
		p.API.LogError("Failed to get team connections",
			"error_code", errcode.ServiceTeardownGetTeamConnsFailed,
			"team_id", channel.TeamId, "error", err.Error())
		return nil, &apiError{Message: "failed to get team connections", Status: 500}
	}

	allConns := p.getAllConnectionNames()
	connMap := p.getConnectionMap()
	configSet := make(map[string]store.TeamConnection, len(allConns))
	connSet := make(map[string]store.TeamConnection)
	for _, tc := range allConns {
		key := connKey(tc)
		configSet[key] = tc
	}
	for _, tc := range teamConns {
		key := connKey(tc)
		connSet[key] = tc
	}
	for _, tc := range channelConns {
		key := connKey(tc)
		if _, exists := connSet[key]; !exists {
			connSet[key] = tc
		}
	}

	relevantKeys := make([]string, 0, len(connSet))
	for key := range connSet {
		relevantKeys = append(relevantKeys, key)
	}
	sort.Strings(relevantKeys)

	channelLinkedSet := make(map[string]struct{}, len(channelConns))
	for _, tc := range channelConns {
		channelLinkedSet[connKey(tc)] = struct{}{}
	}

	cfg := p.getConfiguration()
	checkChanPending := cfg.isChannelRequestMode() && callingUser != nil

	statuses := make([]ConnectionStatus, 0, len(relevantKeys))
	for _, key := range relevantKeys {
		tc := connSet[key]
		_, inConfig := configSet[key]
		_, isLinked := channelLinkedSet[key]
		cc := connMap[key]
		cs := ConnectionStatus{
			Name:                tc.Connection,
			Direction:           tc.Direction,
			Provider:            cc.Provider,
			Linked:              isLinked,
			Orphaned:            !inConfig,
			RemoteTeamName:      tc.RemoteTeamName,
			FileTransferEnabled: cc.FileTransferEnabled,
			FileFilterMode:      cc.FileFilterMode,
			FileFilterTypes:     cc.FileFilterTypes,
			MessageFormat:       statusMessageFormat(tc.Direction, cc.MessageFormat),
		}
		if checkChanPending && !isLinked {
			req, reqErr := p.kvstore.GetChannelConnectionRequest(channelID, key)
			if reqErr != nil {
				p.API.LogWarn("Failed to check pending channel connection request",
					"error_code", errcode.ChanRequestGetFailed,
					"channel_id", channelID, "conn_key", key, "error", reqErr.Error())
			} else if req != nil {
				cs.RequestPending = true
			}
		}
		statuses = append(statuses, cs)
	}

	resp := &ChannelStatusResponse{
		ChannelID:          channel.Id,
		ChannelName:        channel.Name,
		ChannelDisplayName: channel.DisplayName,
		TeamName:           team.DisplayName,
		TeamConnections:    statuses,
	}

	if callingUser != nil && cfg.isChannelRequestMode() && !p.isTeamAdminDirect(callingUser.Id, channel.TeamId) {
		resp.ChannelRequestMode = true
	}

	return resp, nil
}

// initChannelForCrossGuard links a connection to a channel. If the channel did
// not previously have any connections, it also marks the channel as shared and
// posts an announcement. Returns (channel, alreadyLinked, error).
func (p *Plugin) initChannelForCrossGuard(user *model.User, channelID string, conn store.TeamConnection) (*model.Channel, bool, *apiError) {
	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		return nil, false, &apiError{Message: "channel not found", Status: 404}
	}

	teamConns, err := p.kvstore.GetTeamConnections(channel.TeamId)
	if err != nil {
		p.API.LogError("Failed to get team connections",
			"error_code", errcode.ServiceInitChanGetTeamConnsFailed,
			"team_id", channel.TeamId, "error", err.Error())
		return nil, false, &apiError{Message: "failed to check team initialization state", Status: 500}
	}

	teamHasConn := false
	for _, tc := range teamConns {
		if tc.Matches(conn) {
			teamHasConn = true
			break
		}
	}
	if !teamHasConn {
		if _, _, svcErr := p.initTeamForCrossGuard(user, channel.TeamId, conn); svcErr != nil {
			return nil, false, svcErr
		}
	}

	existing, err := p.kvstore.GetChannelConnections(channelID)
	if err != nil {
		p.API.LogError("Failed to get channel connections",
			"error_code", errcode.ServiceInitChanGetChanConnsFailed,
			"channel_id", channelID, "error", err.Error())
		return nil, false, &apiError{Message: "failed to check channel connection state", Status: 500}
	}

	for _, tc := range existing {
		if tc.Matches(conn) {
			// Already linked in Cross Guard's KV. Re-run shareChannelForRemote
			// so any framework state that was removed externally (e.g. via
			// /share-channel unshare or uninvite) is recreated. Both
			// ShareChannel and InviteRemoteToChannel are idempotent on the
			// server: the framework checks HasRemote and returns nil if the
			// remote is already invited, and ShareChannel errors are treated
			// as "may already be shared".
			if shareErr := p.shareChannelForRemote(channel, conn, user.Id); shareErr != nil {
				return nil, false, &apiError{
					Message: fmt.Sprintf("Channel is linked but framework state could not be ensured: %s", shareErr.Error()),
					Status:  500,
				}
			}
			return channel, true, nil
		}
	}

	if addErr := p.kvstore.AddChannelConnection(channelID, conn); addErr != nil {
		p.API.LogError("Failed to add channel connection",
			"error_code", errcode.ServiceAddChanConnFailed,
			"channel_id", channelID, "conn", connKey(conn), "error", addErr.Error())
		return nil, false, &apiError{Message: "failed to save channel connection state", Status: 500}
	}

	updated, err := p.kvstore.GetChannelConnections(channelID)
	if err != nil {
		p.API.LogError("Failed to re-read channel connections",
			"error_code", errcode.ServiceInitChanReReadConnsFailed,
			"channel_id", channelID, "error", err.Error())
		return nil, false, &apiError{Message: "failed to save channel connection state", Status: 500}
	}

	if shareErr := p.shareChannelForRemote(channel, conn, user.Id); shareErr != nil {
		// Roll back the channel connection we just added so the user can
		// retry init-channel cleanly once the underlying problem is
		// resolved. ShareChannel is left in place because it is idempotent
		// and may already be shared by another connection.
		if rmErr := p.kvstore.RemoveChannelConnection(channelID, conn); rmErr != nil {
			p.API.LogError("Failed to roll back channel connection after share failure",
				"error_code", errcode.ServiceShareForRemoteRollback,
				"channel_id", channelID, "conn", connKey(conn), "error", rmErr.Error())
		}
		return nil, false, &apiError{
			Message: fmt.Sprintf("Channel connection setup failed: %s. The connection has been rolled back; retry once the issue is resolved.", shareErr.Error()),
			Status:  500,
		}
	}

	p.publishChannelConnectionUpdate(channelID, updated)

	freshChannel, appErr := p.API.GetChannel(channelID)
	if appErr == nil {
		freshChannel.Header = addCrossguardHeaderPrefix(freshChannel.Header)
		if _, appErr := p.API.UpdateChannel(freshChannel); appErr != nil {
			p.API.LogWarn("Failed to update channel header with CrossGuard prefix",
				"error_code", errcode.ServiceChanHeaderPrefixFailed,
				"channel_id", channelID, "error", appErr.Error())
		}
	}

	displayName := connKey(conn)
	post := &model.Post{
		UserId:    p.botUserID,
		ChannelId: channel.Id,
		Message:   fmt.Sprintf("Cross Guard connection `%s` linked to this channel by @%s. (channel ID: %s, channel name: %s)", displayName, user.Username, channel.Id, channel.Name),
	}
	if _, appErr := p.API.CreatePost(post); appErr != nil {
		p.API.LogWarn("Failed to post channel init message",
			"error_code", errcode.ServicePostChanInitMsgFailed,
			"error", appErr.Error())
	}

	// Auto-cancel any pending channel request for this connection.
	if p.getConfiguration().isChannelRequestMode() {
		p.cancelPendingChannelConnectionRequest(channelID, connKey(conn))
	}

	return channel, false, nil
}

// teardownChannelForCrossGuard unlinks a connection from a channel. If it was
// the last connection, the channel connections are deleted and the channel is
// unmarked as shared.
func (p *Plugin) teardownChannelForCrossGuard(user *model.User, channelID string, conn store.TeamConnection) (*model.Channel, *apiError) {
	channel, appErr := p.API.GetChannel(channelID)
	if appErr != nil {
		return nil, &apiError{Message: "channel not found", Status: 404}
	}

	existing, err := p.kvstore.GetChannelConnections(channelID)
	if err != nil {
		p.API.LogError("Failed to get channel connections",
			"error_code", errcode.ServiceRemoveChanGetConnsFailed,
			"channel_id", channelID, "error", err.Error())
		return nil, &apiError{Message: "failed to check channel connection state", Status: 500}
	}

	if len(existing) == 0 {
		return channel, nil
	}

	found := false
	for _, tc := range existing {
		if tc.Matches(conn) {
			found = true
			break
		}
	}
	if !found {
		return nil, &apiError{Message: fmt.Sprintf("connection %q is not linked to this channel", connKey(conn)), Status: 400}
	}

	if removeErr := p.kvstore.RemoveChannelConnection(channelID, conn); removeErr != nil {
		p.API.LogError("Failed to remove channel connection",
			"error_code", errcode.ServiceRemoveChanConnFailed,
			"channel_id", channelID, "conn", connKey(conn), "error", removeErr.Error())
		return nil, &apiError{Message: "failed to remove channel connection", Status: 500}
	}

	updated, err := p.kvstore.GetChannelConnections(channelID)
	if err != nil {
		p.API.LogError("Failed to re-read channel connections",
			"error_code", errcode.ServiceRemoveChanReReadConnsFailed,
			"channel_id", channelID, "error", err.Error())
		return nil, &apiError{Message: "failed to check channel connection state", Status: 500}
	}

	p.uninviteRemoteFromChannel(channelID, conn)

	if len(updated) == 0 {
		if delErr := p.kvstore.DeleteChannelConnections(channelID); delErr != nil {
			p.API.LogError("Failed to delete channel connections",
				"error_code", errcode.ServiceDeleteChanConnsFailed,
				"channel_id", channelID, "error", delErr.Error())
			return nil, &apiError{Message: "failed to remove channel connections", Status: 500}
		}

		// Only unshare when we had at least one remote registered for this
		// channel. Without remoteIDs (e.g. plugin not yet activated, or no
		// remotes configured) ShareChannel was never called, so UnshareChannel
		// would be a no-op at best and fail at worst.
		if len(p.remoteIDs) > 0 {
			if _, appErr := p.API.UnshareChannel(channelID); appErr != nil {
				p.API.LogWarn("Failed to unshare channel",
					"error_code", errcode.ServiceUnshareFailed,
					"channel_id", channelID, "error", appErr.Error())
			}
		}

		freshChannel, fErr := p.API.GetChannel(channelID)
		if fErr == nil {
			freshChannel.Header = removeCrossguardHeaderPrefix(freshChannel.Header)
			if _, appErr := p.API.UpdateChannel(freshChannel); appErr != nil {
				p.API.LogWarn("Failed to remove CrossGuard prefix from channel header",
					"error_code", errcode.ServiceChanHeaderRemovePrefixFailed,
					"channel_id", channelID, "error", appErr.Error())
			}
		}

		p.publishChannelConnectionUpdate(channelID, nil)
	} else {
		p.publishChannelConnectionUpdate(channelID, updated)
	}

	displayName := connKey(conn)
	msg := fmt.Sprintf("Cross Guard connection `%s` unlinked from this channel by @%s.", displayName, user.Username)
	if len(updated) == 0 {
		msg += " All relays for this channel are now inactive."
	}
	post := &model.Post{
		UserId:    p.botUserID,
		ChannelId: channel.Id,
		Message:   msg,
	}
	if _, appErr := p.API.CreatePost(post); appErr != nil {
		p.API.LogWarn("Failed to post channel teardown message",
			"error_code", errcode.ServicePostChanTeardownMsgFailed,
			"error", appErr.Error())
	}

	return channel, nil
}

// teardownTeamForCrossGuard unlinks a connection from a team. If it was the
// last connection, the team is removed from the initialized list.
func (p *Plugin) teardownTeamForCrossGuard(user *model.User, teamID string, conn store.TeamConnection) (*model.Team, *apiError) {
	team, appErr := p.API.GetTeam(teamID)
	if appErr != nil {
		return nil, &apiError{Message: "team not found", Status: 404}
	}

	existing, err := p.kvstore.GetTeamConnections(teamID)
	if err != nil {
		p.API.LogError("Failed to get team connections",
			"error_code", errcode.ServiceTeardownTeamGetConnsFailed,
			"team_id", teamID, "error", err.Error())
		return nil, &apiError{Message: "failed to check team initialization state", Status: 500}
	}

	if len(existing) == 0 {
		return team, nil
	}

	found := false
	for _, tc := range existing {
		if tc.Matches(conn) {
			found = true
			break
		}
	}
	if !found {
		return nil, &apiError{Message: fmt.Sprintf("connection %q is not linked to this team", connKey(conn)), Status: 400}
	}

	if removeErr := p.kvstore.RemoveTeamConnection(teamID, conn); removeErr != nil {
		p.API.LogError("Failed to remove team connection",
			"error_code", errcode.ServiceRemoveTeamConnFailed,
			"team_id", teamID, "conn", connKey(conn), "error", removeErr.Error())
		return nil, &apiError{Message: "failed to remove team connection", Status: 500}
	}

	updated, err := p.kvstore.GetTeamConnections(teamID)
	if err != nil {
		p.API.LogError("Failed to re-read team connections",
			"error_code", errcode.ServiceTeardownTeamReReadConnsFailed,
			"team_id", teamID, "error", err.Error())
		return nil, &apiError{Message: "failed to check team connection state", Status: 500}
	}

	if len(updated) == 0 {
		if err := p.kvstore.RemoveInitializedTeamID(teamID); err != nil {
			p.API.LogError("Failed to remove team from initialized list",
				"error_code", errcode.ServiceRemoveTeamInitializedFailed,
				"team_id", teamID, "error", err.Error())
			return nil, &apiError{Message: "failed to remove team from initialized list", Status: 500}
		}
	}

	channel, appErr := p.API.GetChannelByName(teamID, model.DefaultChannelName, false)
	if appErr == nil {
		displayName := connKey(conn)
		msg := fmt.Sprintf("Cross Guard connection `%s` unlinked from this team by @%s.", displayName, user.Username)
		if len(updated) == 0 {
			msg += " All channel relays in this team are now inactive."
		}
		post := &model.Post{
			UserId:    p.botUserID,
			ChannelId: channel.Id,
			Message:   msg,
		}
		if _, appErr := p.API.CreatePost(post); appErr != nil {
			p.API.LogWarn("Failed to post team teardown message",
				"error_code", errcode.ServicePostTeamTeardownMsgFailed,
				"error", appErr.Error())
		}
	}

	return team, nil
}

// getAllConnectionNames returns all connections from config as TeamConnection structs.
func (p *Plugin) getAllConnectionNames() []store.TeamConnection {
	cfg := p.getConfiguration()
	outbound, outErr := cfg.GetOutboundConnections()
	inbound, inErr := cfg.GetInboundConnections()

	var conns []store.TeamConnection
	if outErr != nil {
		p.API.LogWarn("Failed to parse outbound connections",
			"error_code", errcode.ServiceGlobalParseOutConnFailed,
			"error", outErr.Error())
	} else {
		for _, conn := range outbound {
			conns = append(conns, store.TeamConnection{Direction: "outbound", Connection: conn.Name})
		}
	}
	if inErr != nil {
		p.API.LogWarn("Failed to parse inbound connections",
			"error_code", errcode.ServiceGlobalParseInConnFailed,
			"error", inErr.Error())
	} else {
		for _, conn := range inbound {
			conns = append(conns, store.TeamConnection{Direction: "inbound", Connection: conn.Name})
		}
	}
	return conns
}

// getConnectionMap returns a map of "direction:name" to ConnectionConfig for config lookups.
func (p *Plugin) getConnectionMap() map[string]ConnectionConfig {
	cfg := p.getConfiguration()
	outbound, outErr := cfg.GetOutboundConnections()
	inbound, inErr := cfg.GetInboundConnections()

	m := make(map[string]ConnectionConfig, len(outbound)+len(inbound))
	if outErr != nil {
		p.API.LogWarn("Failed to parse outbound connections for map",
			"error_code", errcode.ServiceMapParseOutConnFailed,
			"error", outErr.Error())
	} else {
		for _, conn := range outbound {
			m["outbound:"+conn.Name] = conn
		}
	}
	if inErr != nil {
		p.API.LogWarn("Failed to parse inbound connections for map",
			"error_code", errcode.ServiceMapParseInConnFailed,
			"error", inErr.Error())
	} else {
		for _, conn := range inbound {
			m["inbound:"+conn.Name] = conn
		}
	}
	return m
}

// resolveConnectionName resolves the connection name from the given name and
// available list. If connName is empty and there is exactly one connection,
// it auto-selects it. The user-supplied name may be a full key
// ("outbound:high") or just the bare connection name ("high"); the bare form
// resolves only when unambiguous (exactly one matching direction).
// Returns (resolved connection, available list, error message). A non-empty
// error message means the caller should report it to the user.
func (p *Plugin) resolveConnectionName(connName string, available []store.TeamConnection) (store.TeamConnection, []store.TeamConnection, string) {
	if len(available) == 0 {
		return store.TeamConnection{}, nil, "no connections configured"
	}

	if connName == "" {
		if len(available) == 1 {
			return available[0], available, ""
		}
		return store.TeamConnection{}, available, "multiple connections available, specify connection_name"
	}

	for _, tc := range available {
		if connKey(tc) == connName {
			return tc, available, ""
		}
	}

	// Fall back to a bare-name match. Accept it when exactly one entry has
	// this Connection name; ambiguous bare names report a clear error so
	// the user picks the direction explicitly.
	var matches []store.TeamConnection
	for _, tc := range available {
		if tc.Connection == connName {
			matches = append(matches, tc)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], available, ""
	case 0:
		return store.TeamConnection{}, available, fmt.Sprintf("connection not found: %s", connName)
	default:
		return store.TeamConnection{}, available, fmt.Sprintf("connection name %q is ambiguous, specify direction (e.g. %s)", connName, connKey(matches[0]))
	}
}

// redactConnections strips sensitive fields from connections for the status response.
func redactConnections(outbound, inbound []ConnectionConfig) []RedactedConnection {
	connections := make([]RedactedConnection, 0, len(outbound)+len(inbound))
	for _, conn := range outbound {
		connections = append(connections, redactConnection(conn, "outbound"))
	}
	for _, conn := range inbound {
		connections = append(connections, redactConnection(conn, "inbound"))
	}
	return connections
}

// shareChannelForRemote ensures the channel is registered as shared and the
// remote for the linked connection is invited. ShareChannel errors are
// treated as idempotent (the channel may already be shared). An
// InviteRemoteToChannel failure is returned so the caller can roll back the
// channel connection rather than leaving it half-initialized.
func (p *Plugin) shareChannelForRemote(channel *model.Channel, conn store.TeamConnection, userID string) error {
	remoteID := p.remoteIDs[connKey(conn)]
	if remoteID == "" {
		return nil
	}

	if _, err := p.API.ShareChannel(&model.SharedChannel{
		ChannelId:        channel.Id,
		TeamId:           channel.TeamId,
		Home:             true,
		ShareName:        channel.Name,
		ShareDisplayName: channel.DisplayName,
		CreatorId:        userID,
	}); err != nil {
		p.API.LogDebug("ShareChannel returned error (may already be shared)",
			"error_code", errcode.ServiceShareChannelFailed,
			"channel_id", channel.Id, "error", err.Error())
	}

	if err := p.API.InviteRemoteToChannel(channel.Id, remoteID, userID, true); err != nil {
		p.API.LogError("Failed to invite remote to channel",
			"error_code", errcode.ServiceInviteRemoteFailed,
			"channel_id", channel.Id, "remote_id", remoteID, "error", err.Error())
		return fmt.Errorf("failed to invite remote to channel: %w", err)
	}
	return nil
}

// uninviteRemoteFromChannel uninvites the remote associated with the given
// connection from the channel. Errors are logged but do not block teardown.
// Paired connections share a remoteID, so callers should ensure the other
// direction is not still linked before invoking this; otherwise the second
// teardown is a noop.
func (p *Plugin) uninviteRemoteFromChannel(channelID string, conn store.TeamConnection) {
	remoteID := p.remoteIDs[connKey(conn)]
	if remoteID == "" {
		return
	}
	if err := p.API.UninviteRemoteFromChannel(channelID, remoteID); err != nil {
		p.API.LogWarn("Failed to uninvite remote from channel",
			"error_code", errcode.ServiceUninviteRemoteFailed,
			"channel_id", channelID, "remote_id", remoteID, "error", err.Error())
	}
}

func redactConnection(conn ConnectionConfig, direction string) RedactedConnection {
	rc := RedactedConnection{
		Name:                conn.Name,
		Direction:           direction,
		Provider:            conn.Provider,
		FileTransferEnabled: conn.FileTransferEnabled,
		FileFilterMode:      conn.FileFilterMode,
		FileFilterTypes:     conn.FileFilterTypes,
		MessageFormat:       statusMessageFormat(direction, conn.MessageFormat),
	}
	if conn.NATS != nil {
		rc.Address = conn.NATS.Address
		rc.AuthType = conn.NATS.AuthType
		rc.Subject = conn.NATS.Subject
	}
	if conn.AzureQueue != nil {
		rc.QueueName = conn.AzureQueue.QueueName
		rc.BlobContainerName = conn.AzureQueue.BlobContainerName
		rc.AuthMode = conn.AzureQueue.AuthMode
		rc.AzureCloud = conn.AzureQueue.AzureCloud
		rc.TenantID = conn.AzureQueue.TenantID
		rc.ClientID = conn.AzureQueue.ClientID
	}
	if conn.AzureBlob != nil {
		rc.BlobContainerName = conn.AzureBlob.BlobContainerName
		rc.AuthMode = conn.AzureBlob.AuthMode
		rc.AzureCloud = conn.AzureBlob.AzureCloud
		rc.TenantID = conn.AzureBlob.TenantID
		rc.ClientID = conn.AzureBlob.ClientID
	}
	if conn.AzureServiceBus != nil {
		// Only safe-to-expose fields: queue name, blob container, SP
		// identifiers, namespace, cloud. ConnectionString, BlobAccountKey,
		// and ClientSecret MUST never appear on the redacted view.
		rc.QueueName = conn.AzureServiceBus.QueueName
		rc.BlobContainerName = conn.AzureServiceBus.BlobContainerName
		rc.AuthMode = conn.AzureServiceBus.AuthMode
		rc.AzureCloud = conn.AzureServiceBus.AzureCloud
		rc.TenantID = conn.AzureServiceBus.TenantID
		rc.ClientID = conn.AzureServiceBus.ClientID
		rc.ServiceBusNamespace = conn.AzureServiceBus.ServiceBusNamespace
	}
	return rc
}
