package main

import (
	"fmt"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"

	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/errcode"
	"github.com/MattermostFederal/mattermost-plugin-crossguard/server/store"
)

func fileTransferLabel(enabled bool, filterMode, filterTypes string) string {
	if !enabled {
		return "files off"
	}
	switch filterMode {
	case "allow":
		return "files on, allow: " + filterTypes
	case "deny":
		return "files on, deny: " + filterTypes
	default:
		return "files on"
	}
}

func connectionLabel(fileEnabled bool, filterMode, filterTypes string) string {
	return "xml, " + fileTransferLabel(fileEnabled, filterMode, filterTypes)
}

func providerLabel(provider string) string {
	if provider == "" {
		return ProviderNATS
	}
	return provider
}

func providerDetails(conn RedactedConnection) string {
	switch conn.Provider {
	case ProviderAzureQueue:
		parts := []string{"queue: " + conn.QueueName}
		if conn.BlobContainerName != "" {
			parts = append(parts, "blob: "+conn.BlobContainerName)
		}
		return strings.Join(parts, ", ")
	case ProviderAzureServiceBus:
		parts := []string{"queue: " + conn.QueueName}
		if conn.BlobContainerName != "" {
			parts = append(parts, "blob: "+conn.BlobContainerName)
		}
		return strings.Join(parts, ", ")
	case ProviderAzureBlob:
		return "blob: " + conn.BlobContainerName
	case ProviderNATS, "":
		parts := []string{}
		if conn.Address != "" {
			parts = append(parts, conn.Address)
		}
		if conn.Subject != "" {
			parts = append(parts, "subject: "+conn.Subject)
		}
		if conn.AuthType != "" {
			parts = append(parts, "auth: "+conn.AuthType)
		}
		return strings.Join(parts, ", ")
	default:
		return conn.Provider
	}
}

func fileTransferLabelEmoji(enabled bool, filterMode, filterTypes string) string {
	if !enabled {
		return ":x: Off"
	}
	switch filterMode {
	case "allow":
		return ":white_check_mark: On (allow: " + filterTypes + ")"
	case "deny":
		return ":white_check_mark: On (deny: " + filterTypes + ")"
	default:
		return ":white_check_mark: On"
	}
}

const (
	commandTrigger           = "crossguard"
	actionInitTeam           = "init-team"
	actionTeardownTeam       = "teardown-team"
	actionInitChannel        = "init-channel"
	actionTeardownChannel    = "teardown-channel"
	actionResetPrompt        = "reset-prompt"
	actionResetChannelPrompt = "reset-channel-prompt"
	actionRewriteTeam        = "rewrite-team"
	actionHelp               = "help"
)

func (p *Plugin) registerCommand() error {
	return p.API.RegisterCommand(&model.Command{
		Trigger:          commandTrigger,
		AutoComplete:     true,
		AutoCompleteDesc: "Cross Guard commands",
		AutoCompleteHint: "[init-team|init-channel|teardown-team|teardown-channel|reset-prompt|reset-channel-prompt|rewrite-team|status|help]",
		AutocompleteData: getAutocompleteData(),
	})
}

func getAutocompleteData() *model.AutocompleteData {
	cmd := model.NewAutocompleteData(commandTrigger, "[command]", "Cross Guard commands")

	connectionURL := func(action string) string {
		return "/plugins/" + manifest.Id + "/api/v1/autocomplete/connections/" + action
	}

	initTeam := model.NewAutocompleteData("init-team", "[connection-name]", "Link a connection to this team (requires team admin or system admin)")
	initTeam.AddDynamicListArgument("Configured connections from System Console", connectionURL(actionInitTeam), false)
	cmd.AddCommand(initTeam)

	initChannel := model.NewAutocompleteData("init-channel", "[connection-name]", "Link a connection to this channel (requires channel admin or higher)")
	initChannel.AddDynamicListArgument("Connections linked to this team", connectionURL(actionInitChannel), false)
	cmd.AddCommand(initChannel)

	teardownTeam := model.NewAutocompleteData("teardown-team", "[connection-name]", "Unlink a connection from this team (requires team admin or system admin)")
	teardownTeam.AddDynamicListArgument("Connections linked to this team", connectionURL(actionTeardownTeam), false)
	cmd.AddCommand(teardownTeam)

	teardownChannel := model.NewAutocompleteData("teardown-channel", "[connection-name]", "Unlink a connection from this channel (requires channel admin or higher)")
	teardownChannel.AddDynamicListArgument("Connections linked to this channel", connectionURL(actionTeardownChannel), false)
	cmd.AddCommand(teardownChannel)

	resetPrompt := model.NewAutocompleteData("reset-prompt", "<connection-name>", "Clear a blocked or pending connection prompt for this team")
	cmd.AddCommand(resetPrompt)

	resetChannelPrompt := model.NewAutocompleteData("reset-channel-prompt", "<connection-name>", "Clear a blocked or pending connection prompt for this channel")
	cmd.AddCommand(resetChannelPrompt)

	rewriteTeam := model.NewAutocompleteData("rewrite-team", "[connection-name] [remote-team-name]",
		"Set or clear a remote team name rewrite for an inbound connection on this team")
	cmd.AddCommand(rewriteTeam)

	status := model.NewAutocompleteData("status", "", "Check Cross Guard status for this team")
	cmd.AddCommand(status)

	help := model.NewAutocompleteData("help", "", "Show detailed help for all Cross Guard commands")
	cmd.AddCommand(help)

	return cmd
}

func (p *Plugin) ExecuteCommand(_ *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	parts := strings.Fields(args.Command)
	if len(parts) < 2 {
		return respondEphemeral("Run `/%s help` to see all available commands.", commandTrigger), nil
	}

	subcommand := parts[1]
	switch subcommand {
	case "init-team":
		return p.executeInitTeam(args), nil
	case "init-channel":
		return p.executeInitChannel(args), nil
	case "teardown-team":
		return p.executeTeardownTeam(args), nil
	case "teardown-channel":
		return p.executeTeardownChannel(args), nil
	case actionResetPrompt:
		return p.executeResetPrompt(args), nil
	case actionResetChannelPrompt:
		return p.executeResetChannelPrompt(args), nil
	case "status":
		return p.executeStatus(args), nil
	case actionRewriteTeam:
		return p.executeRewriteTeam(args), nil
	case actionHelp:
		return p.executeHelp(), nil
	default:
		return respondEphemeral("Unknown subcommand: `%s`. Run `/%s help` to see all available commands.", subcommand, commandTrigger), nil
	}
}

func (p *Plugin) executeHelp() *model.CommandResponse {
	var sb strings.Builder

	sb.WriteString("#### Cross Guard Help\n\n")

	sb.WriteString("**Quick Reference:**\n\n")
	sb.WriteString("| Command | Description | Permission |\n")
	sb.WriteString("|:--------|:------------|:-----------|\n")
	sb.WriteString("| `init-team [name]` | Link a connection to this team | Team Admin |\n")
	sb.WriteString("| `init-channel [name]` | Link a connection to this channel | Channel Admin |\n")
	sb.WriteString("| `teardown-team [name]` | Unlink a connection from this team | Team Admin |\n")
	sb.WriteString("| `teardown-channel [name]` | Unlink a connection from this channel | Channel Admin |\n")
	sb.WriteString("| `reset-prompt <name>` | Clear a pending team connection prompt | Team Admin |\n")
	sb.WriteString("| `reset-channel-prompt <name>` | Clear a pending channel connection prompt | Team Admin |\n")
	sb.WriteString("| `rewrite-team [name] [team]` | Set or clear a remote team name rewrite | Team Admin |\n")
	sb.WriteString("| `status` | Show Cross Guard status for this team | Any member |\n")
	sb.WriteString("| `help` | Show this help message | Any member |\n")

	sb.WriteString("\n---\n\n")
	sb.WriteString("**Detailed Commands:**\n\n")

	sb.WriteString("##### `/crossguard init-team [connection-name]`\n")
	sb.WriteString("Link a connection to the current team, enabling cross-domain relay.\n")
	sb.WriteString("- **Permission:** Team Admin or System Admin\n")
	sb.WriteString("- **Arguments:** `connection-name` (optional if only one connection is configured)\n")
	sb.WriteString("- **Example:** `/crossguard init-team myconn`\n")
	sb.WriteString("- If multiple connections exist and no name is given, a selection dialog will appear.\n\n")

	sb.WriteString("##### `/crossguard init-channel [connection-name]`\n")
	sb.WriteString("Link a connection to the current channel. The team must be initialized first.\n")
	sb.WriteString("- **Permission:** Channel Admin, Team Admin, or System Admin\n")
	sb.WriteString("- **Arguments:** `connection-name` (optional if only one connection is configured)\n")
	sb.WriteString("- **Example:** `/crossguard init-channel myconn`\n")
	sb.WriteString("- If multiple connections exist and no name is given, a selection dialog will appear.\n\n")

	sb.WriteString("##### `/crossguard teardown-team [connection-name]`\n")
	sb.WriteString("Unlink a connection from the current team.\n")
	sb.WriteString("- **Permission:** Team Admin or System Admin\n")
	sb.WriteString("- **Arguments:** `connection-name` (optional if only one connection is linked)\n")
	sb.WriteString("- **Example:** `/crossguard teardown-team myconn`\n\n")

	sb.WriteString("##### `/crossguard teardown-channel [connection-name]`\n")
	sb.WriteString("Unlink a connection from the current channel.\n")
	sb.WriteString("- **Permission:** Channel Admin, Team Admin, or System Admin\n")
	sb.WriteString("- **Arguments:** `connection-name` (optional if only one connection is linked)\n")
	sb.WriteString("- **Example:** `/crossguard teardown-channel myconn`\n\n")

	sb.WriteString("##### `/crossguard reset-prompt <connection-name>`\n")
	sb.WriteString("Clear a blocked or pending inbound connection prompt for the current team. A new prompt will appear on the next inbound message.\n")
	sb.WriteString("- **Permission:** Team Admin or System Admin\n")
	sb.WriteString("- **Arguments:** `connection-name` (required)\n")
	sb.WriteString("- **Example:** `/crossguard reset-prompt myconn`\n\n")

	sb.WriteString("##### `/crossguard reset-channel-prompt <connection-name>`\n")
	sb.WriteString("Clear a blocked or pending inbound connection prompt for the current channel.\n")
	sb.WriteString("- **Permission:** Team Admin or System Admin\n")
	sb.WriteString("- **Arguments:** `connection-name` (required)\n")
	sb.WriteString("- **Example:** `/crossguard reset-channel-prompt myconn`\n\n")

	sb.WriteString("##### `/crossguard rewrite-team [connection-name] [remote-team-name]`\n")
	sb.WriteString("Set or clear a remote team name rewrite for an inbound connection. When set, inbound messages with the specified remote team name will route to this team.\n")
	sb.WriteString("- **Permission:** Team Admin or System Admin\n")
	sb.WriteString("- **Arguments:** `connection-name` (required if multiple inbound connections), `remote-team-name` (omit to clear)\n")
	sb.WriteString("- **Example:** `/crossguard rewrite-team myconn remote-team`\n")
	sb.WriteString("- **Example (clear):** `/crossguard rewrite-team myconn`\n\n")

	sb.WriteString("##### `/crossguard status`\n")
	sb.WriteString("Show the Cross Guard status for the current team and channel. System Admins see a global overview of all initialized teams and connections.\n")
	sb.WriteString("- **Permission:** Any team member (System Admins see global status)\n\n")

	sb.WriteString("---\n\n")

	sb.WriteString("**Getting Started:**\n")
	sb.WriteString("1. Configure connections in **System Console > Plugins > Cross Guard**\n")
	sb.WriteString("2. Run `/crossguard init-team` in the team you want to enable\n")
	sb.WriteString("3. Run `/crossguard init-channel` in each channel that should relay messages\n")
	sb.WriteString("4. Run `/crossguard status` to verify the setup\n\n")

	sb.WriteString("**Permissions:**\n")
	sb.WriteString("- **System Admin:** Full access to all commands\n")
	sb.WriteString("- **Team Admin:** Can manage team and channel connections, reset prompts, set rewrites\n")
	sb.WriteString("- **Channel Admin:** Can link/unlink channels within initialized teams\n")
	sb.WriteString("- When `RestrictToSystemAdmins` is enabled, only System Admins can run commands\n")

	sb.WriteString("\n---\n\n")
	sb.WriteString("**Full Documentation:** [Cross Guard Help](/plugins/crossguard/public/help/help.html)\n")

	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         sb.String(),
	}
}

func (p *Plugin) executeInitTeam(args *model.CommandArgs) *model.CommandResponse {
	if !p.isTeamAdminOrSystemAdmin(args.UserId, args.TeamId) {
		if p.isTeamAdminInRequestMode(args.UserId, args.TeamId) {
			return p.executeInitTeamRequest(args)
		}
		return respondEphemeral("You don't have permissions to run this command. You must be a team admin or system admin.")
	}

	user, appErr := p.getUser(args.UserId)
	if appErr != nil {
		return respondEphemeral("Failed to look up user.")
	}

	parts := strings.Fields(args.Command)
	inputName := ""
	if len(parts) >= 3 {
		inputName = parts[2]
	}

	connName, allConns, resolveErr := p.resolveConnectionName(inputName, p.getAllConnectionNames())
	if resolveErr != "" {
		if len(allConns) == 0 {
			return respondEphemeral("No connections configured. Add connections in the System Console first.")
		}
		if inputName == "" && len(allConns) > 1 {
			p.openConnectionDialog(args.TriggerId, args.TeamId, allConns, actionInitTeam)
			return &model.CommandResponse{}
		}
		return respondEphemeral("%s\n\nAvailable connections: %s", resolveErr, strings.Join(connectionDisplayNames(allConns), ", "))
	}

	team, alreadyLinked, svcErr := p.initTeamForCrossGuard(user, args.TeamId, connName)
	if svcErr != nil {
		return respondEphemeral("%s", svcErr.Message)
	}

	if alreadyLinked {
		return respondEphemeral("Connection `%s` is already linked to this team. (team ID: %s, team name: %s)", connKey(connName), team.Id, team.Name)
	}

	return &model.CommandResponse{}
}

func (p *Plugin) executeInitTeamRequest(args *model.CommandArgs) *model.CommandResponse {
	user, appErr := p.getUser(args.UserId)
	if appErr != nil {
		return respondEphemeral("Failed to look up user.")
	}

	parts := strings.Fields(args.Command)
	inputName := ""
	if len(parts) >= 3 {
		inputName = parts[2]
	}

	conn, allConns, resolveErr := p.resolveConnectionName(inputName, p.getAllConnectionNames())
	if resolveErr != "" {
		if len(allConns) == 0 {
			return respondEphemeral("No connections configured. Add connections in the System Console first.")
		}
		if inputName == "" && len(allConns) > 1 {
			p.openConnectionDialog(args.TriggerId, args.TeamId, allConns, actionInitTeam)
			return &model.CommandResponse{}
		}
		return respondEphemeral("%s\n\nAvailable connections: %s", resolveErr, strings.Join(connectionDisplayNames(allConns), ", "))
	}

	msg, err := p.createConnectionRequest(user, args.TeamId, conn)
	if err != nil {
		return respondEphemeral("%s", err.Error())
	}

	return respondEphemeral("%s", msg)
}

func (p *Plugin) executeStatus(args *model.CommandArgs) *model.CommandResponse {
	user, appErr := p.getUser(args.UserId)
	if appErr != nil {
		return respondEphemeral("Failed to look up user: %s", appErr.Error())
	}

	if user.IsSystemAdmin() {
		return p.executeStatusSystemAdmin(args.ChannelId)
	}

	return p.executeStatusTeam(args.TeamId, args.ChannelId)
}

func (p *Plugin) executeStatusTeam(teamID, channelID string) *model.CommandResponse {
	resp, svcErr := p.getTeamStatus(teamID, nil)
	if svcErr != nil {
		return respondEphemeral("%s", svcErr.Message)
	}

	channelConns, _ := p.kvstore.GetChannelConnections(channelID)

	teamStatus := ":x: No"
	if resp.Initialized {
		teamStatus = ":white_check_mark: Yes"
	}

	channelStatus := ":x: No"
	if len(channelConns) > 0 {
		channelStatus = ":white_check_mark: Yes"
	}

	team, _ := p.API.GetTeam(teamID)
	channel, _ := p.API.GetChannel(channelID)

	teamName := teamID
	teamDisplayName := ""
	if team != nil {
		teamName = team.Name
		teamDisplayName = team.DisplayName
	}

	channelName := channelID
	channelDisplayName := ""
	if channel != nil {
		channelName = channel.Name
		channelDisplayName = channel.DisplayName
	}

	var sb strings.Builder
	sb.WriteString("#### Cross Guard Status\n\n")

	sb.WriteString("**Channel:**\n\n")
	sb.WriteString("| Channel | Name | ID | Relay Enabled |\n")
	sb.WriteString("|:--------|:-----|:---|:--------------|\n")
	fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n", channelDisplayName, channelName, channelID, channelStatus)

	sb.WriteString("\n**Team:**\n\n")
	sb.WriteString("| Team | Name | ID | Initialized |\n")
	sb.WriteString("|:-----|:-----|:---|:------------|\n")
	fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n", teamDisplayName, teamName, teamID, teamStatus)

	if len(resp.Connections) > 0 {
		sb.WriteString("\n**Team Connections:**\n\n")
		for _, cs := range resp.Connections {
			if !cs.Linked {
				continue
			}
			label := connectionLabel(cs.FileTransferEnabled, cs.FileFilterMode, cs.FileFilterTypes)
			fmt.Fprintf(&sb, "- `%s:%s` (%s, %s)\n", cs.Direction, cs.Name, providerLabel(cs.Provider), label)
		}
	}

	if len(channelConns) > 0 {
		connMap := p.getConnectionMap()
		sb.WriteString("\n**Channel Connections:**\n\n")
		for _, tc := range channelConns {
			key := connKey(tc)
			cc := connMap[key]
			clabel := connectionLabel(cc.FileTransferEnabled, cc.FileFilterMode, cc.FileFilterTypes)
			fmt.Fprintf(&sb, "- `%s` (%s, %s)\n", key, providerLabel(cc.Provider), clabel)
		}
	}

	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         sb.String(),
	}
}

func (p *Plugin) executeStatusSystemAdmin(channelID string) *model.CommandResponse {
	resp, svcErr := p.getGlobalStatus()
	if svcErr != nil {
		return respondEphemeral("%s", svcErr.Message)
	}

	if len(resp.Teams) == 0 {
		return respondEphemeral("No teams have been initialized. Run `/%s init-team` in a team to get started.", commandTrigger)
	}

	var sb strings.Builder
	sb.WriteString("#### Cross Guard Status\n\n")

	channelConns, _ := p.kvstore.GetChannelConnections(channelID)
	channelStatus := ":x: No"
	if len(channelConns) > 0 {
		channelStatus = ":white_check_mark: Yes"
	}
	channel, _ := p.API.GetChannel(channelID)
	channelName := channelID
	channelDisplayName := ""
	if channel != nil {
		channelName = channel.Name
		channelDisplayName = channel.DisplayName
	}

	sb.WriteString("**Channel:**\n\n")
	sb.WriteString("| Channel | Name | ID | Relay Enabled |\n")
	sb.WriteString("|:--------|:-----|:---|:--------------|\n")
	fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n", channelDisplayName, channelName, channelID, channelStatus)

	if len(channelConns) > 0 {
		connMap := p.getConnectionMap()
		sb.WriteString("\n**Channel Connections:**\n\n")
		for _, tc := range channelConns {
			key := connKey(tc)
			cc := connMap[key]
			clabel := connectionLabel(cc.FileTransferEnabled, cc.FileFilterMode, cc.FileFilterTypes)
			fmt.Fprintf(&sb, "- `%s` (%s, %s)\n", key, providerLabel(cc.Provider), clabel)
		}
	}

	sb.WriteString("\n**Initialized Teams:**\n\n")
	sb.WriteString("| Team | Team ID | Team Name | Linked Connections |\n")
	sb.WriteString("|:-----|:--------|:----------|:-------------------|\n")

	for _, team := range resp.Teams {
		conns := "(none)"
		if len(team.LinkedConnections) > 0 {
			conns = strings.Join(connectionDisplayNames(team.LinkedConnections), ", ")
		}
		fmt.Fprintf(&sb, "| %s | %s | %s | %s |\n", team.DisplayName, team.TeamID, team.TeamName, conns)
	}

	if len(resp.Warnings) > 0 {
		sb.WriteString("\n**Warning:** Failed to parse some connection configuration. Check System Console settings.\n")
	}

	if len(resp.Connections) > 0 {
		sb.WriteString("\n**Connections:**\n\n")
		sb.WriteString("| Name | Direction | Provider | Details | Format | Files |\n")
		sb.WriteString("|:-----|:----------|:---------|:--------|:-------|:------|\n")
		for _, conn := range resp.Connections {
			fmt.Fprintf(&sb, "| %s | %s | %s | %s | %s | %s |\n", conn.Name, conn.Direction, providerLabel(conn.Provider), providerDetails(conn), "xml", fileTransferLabelEmoji(conn.FileTransferEnabled, conn.FileFilterMode, conn.FileFilterTypes))
		}
	}

	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         sb.String(),
	}
}

func (p *Plugin) isTeamAdminOrSystemAdmin(userID, teamID string) bool {
	user, appErr := p.getUser(userID)
	if appErr != nil {
		return false
	}

	if user.IsSystemAdmin() {
		return true
	}

	if p.getConfiguration().isRestrictedToSystemAdmins() {
		return false
	}

	member, appErr := p.API.GetTeamMember(teamID, userID)
	if appErr != nil {
		return false
	}

	return member.SchemeAdmin
}

func (p *Plugin) executeInitChannel(args *model.CommandArgs) *model.CommandResponse {
	if !p.isChannelAdminOrHigher(args.UserId, args.ChannelId, args.TeamId) {
		switch {
		case p.canDirectlyManageChannelConns(args.UserId, args.TeamId):
			// Team admins get direct channel access when channel request mode
			// is on (they are the approvers). Fall through to normal flow.
		case p.isChannelAdminInRequestMode(args.UserId, args.ChannelId, args.TeamId):
			return p.executeInitChannelRequest(args)
		default:
			return respondEphemeral("You must be a member of this channel and a channel admin, team admin, or system admin.")
		}
	}

	user, appErr := p.getUser(args.UserId)
	if appErr != nil {
		return respondEphemeral("Failed to look up user.")
	}

	teamConns, err := p.kvstore.GetTeamConnections(args.TeamId)
	if err != nil {
		return respondEphemeral("Failed to check team connections.")
	}
	if len(teamConns) == 0 {
		return respondEphemeral("Team must be initialized first. Run `/%s init-team` first.", commandTrigger)
	}

	parts := strings.Fields(args.Command)
	inputName := ""
	if len(parts) >= 3 {
		inputName = parts[2]
	}

	allConns := p.getAllConnectionNames()
	connName, _, resolveErr := p.resolveConnectionName(inputName, allConns)
	if resolveErr != "" {
		if len(allConns) == 0 {
			return respondEphemeral("No connections configured. Check the System Console settings.")
		}
		if inputName == "" && len(allConns) > 1 {
			p.openConnectionDialog(args.TriggerId, args.ChannelId, allConns, actionInitChannel)
			return &model.CommandResponse{}
		}
		return respondEphemeral("%s\n\nAvailable connections: %s", resolveErr, strings.Join(connectionDisplayNames(allConns), ", "))
	}

	teamHasConn := false
	for _, tc := range teamConns {
		if tc.Matches(connName) {
			teamHasConn = true
			break
		}
	}
	if !teamHasConn {
		return respondEphemeral("Connection `%s` is not linked to this team. Run `/%s init-team` first.", connKey(connName), commandTrigger)
	}

	ch, alreadyLinked, svcErr := p.initChannelForCrossGuard(user, args.ChannelId, connName)
	if svcErr != nil {
		return respondEphemeral("%s", svcErr.Message)
	}

	if alreadyLinked {
		return respondEphemeral("Connection `%s` is already linked to this channel. (channel ID: %s, channel name: %s)", connKey(connName), ch.Id, ch.Name)
	}

	return &model.CommandResponse{}
}

func (p *Plugin) executeTeardownChannel(args *model.CommandArgs) *model.CommandResponse {
	if !p.isChannelAdminOrHigher(args.UserId, args.ChannelId, args.TeamId) {
		if !p.canDirectlyManageChannelConns(args.UserId, args.TeamId) &&
			!p.isChannelAdminInRequestMode(args.UserId, args.ChannelId, args.TeamId) {
			return respondEphemeral("You must be a member of this channel and a channel admin, team admin, or system admin.")
		}
	}

	user, appErr := p.getUser(args.UserId)
	if appErr != nil {
		return respondEphemeral("Failed to look up user.")
	}

	linked, err := p.kvstore.GetChannelConnections(args.ChannelId)
	if err != nil {
		return respondEphemeral("Failed to check channel connections.")
	}
	if len(linked) == 0 {
		return respondEphemeral("No connections are linked to this channel.")
	}

	parts := strings.Fields(args.Command)
	inputName := ""
	if len(parts) >= 3 {
		inputName = parts[2]
	}

	connName, _, resolveErr := p.resolveConnectionName(inputName, linked)
	if resolveErr != "" {
		if inputName == "" && len(linked) > 1 {
			p.openConnectionDialog(args.TriggerId, args.ChannelId, linked, actionTeardownChannel)
			return &model.CommandResponse{}
		}
		return respondEphemeral("%s\n\nLinked connections: %s", resolveErr, strings.Join(connectionDisplayNames(linked), ", "))
	}

	if _, svcErr := p.teardownChannelForCrossGuard(user, args.ChannelId, connName); svcErr != nil {
		return respondEphemeral("%s", svcErr.Message)
	}

	p.cancelPendingChannelConnectionRequest(args.ChannelId, connKey(connName))

	return &model.CommandResponse{}
}

func (p *Plugin) executeInitChannelRequest(args *model.CommandArgs) *model.CommandResponse {
	user, appErr := p.getUser(args.UserId)
	if appErr != nil {
		return respondEphemeral("Failed to look up user.")
	}

	teamConns, err := p.kvstore.GetTeamConnections(args.TeamId)
	if err != nil {
		return respondEphemeral("Failed to check team connections.")
	}
	if len(teamConns) == 0 {
		return respondEphemeral("Team must be initialized first. Run `/%s init-team` first.", commandTrigger)
	}

	parts := strings.Fields(args.Command)
	inputName := ""
	if len(parts) >= 3 {
		inputName = parts[2]
	}

	conn, allConns, resolveErr := p.resolveConnectionName(inputName, teamConns)
	if resolveErr != "" {
		if len(allConns) == 0 {
			return respondEphemeral("No connections available on this team.")
		}
		if inputName == "" && len(allConns) > 1 {
			p.openConnectionDialog(args.TriggerId, args.ChannelId, allConns, actionInitChannel)
			return &model.CommandResponse{}
		}
		return respondEphemeral("%s\n\nAvailable connections: %s", resolveErr, strings.Join(connectionDisplayNames(allConns), ", "))
	}

	msg, crErr := p.createChannelConnectionRequest(user, args.ChannelId, args.TeamId, conn)
	if crErr != nil {
		return respondEphemeral("%s", crErr.Error())
	}

	return respondEphemeral("%s", msg)
}

func (p *Plugin) executeTeardownTeam(args *model.CommandArgs) *model.CommandResponse {
	if !p.isTeamAdminOrSystemAdmin(args.UserId, args.TeamId) {
		if !p.isTeamAdminInRequestMode(args.UserId, args.TeamId) {
			return respondEphemeral("You don't have permissions to run this command. You must be a team admin or system admin.")
		}
	}

	user, appErr := p.getUser(args.UserId)
	if appErr != nil {
		return respondEphemeral("Failed to look up user.")
	}

	linked, err := p.kvstore.GetTeamConnections(args.TeamId)
	if err != nil {
		return respondEphemeral("Failed to check team connections.")
	}
	if len(linked) == 0 {
		return respondEphemeral("No connections are linked to this team.")
	}

	parts := strings.Fields(args.Command)
	inputName := ""
	if len(parts) >= 3 {
		inputName = parts[2]
	}

	connName, _, resolveErr := p.resolveConnectionName(inputName, linked)
	if resolveErr != "" {
		if inputName == "" && len(linked) > 1 {
			p.openConnectionDialog(args.TriggerId, args.TeamId, linked, actionTeardownTeam)
			return &model.CommandResponse{}
		}
		return respondEphemeral("%s\n\nLinked connections: %s", resolveErr, strings.Join(connectionDisplayNames(linked), ", "))
	}

	if _, svcErr := p.teardownTeamForCrossGuard(user, args.TeamId, connName); svcErr != nil {
		return respondEphemeral("%s", svcErr.Message)
	}

	p.cancelPendingConnectionRequest(args.TeamId, connKey(connName))
	// Pending channel requests for this connection are not cascaded here because
	// we lack a channel index. The channel request approve handler validates the
	// team still has the connection and auto-cancels if not.

	return &model.CommandResponse{}
}

func (p *Plugin) executeRewriteTeam(args *model.CommandArgs) *model.CommandResponse {
	if !p.isTeamAdminOrSystemAdmin(args.UserId, args.TeamId) {
		return respondEphemeral("You must be a team admin or system admin.")
	}

	conns, err := p.kvstore.GetTeamConnections(args.TeamId)
	if err != nil {
		return respondEphemeral("Failed to check team connections.")
	}

	var inboundConns []store.TeamConnection
	for _, tc := range conns {
		if tc.Direction == "inbound" {
			inboundConns = append(inboundConns, tc)
		}
	}
	if len(inboundConns) == 0 {
		return respondEphemeral("No inbound connections are linked to this team.")
	}

	parts := strings.Fields(args.Command)
	connName := ""
	if len(parts) >= 3 {
		connName = parts[2]
	}

	var target *store.TeamConnection
	if connName == "" {
		if len(inboundConns) == 1 {
			target = &inboundConns[0]
		} else {
			names := make([]string, len(inboundConns))
			for i, tc := range inboundConns {
				names[i] = tc.Connection
			}
			return respondEphemeral("Multiple inbound connections, specify one: %s", strings.Join(names, ", "))
		}
	} else {
		for i, tc := range inboundConns {
			if tc.Connection == connName {
				target = &inboundConns[i]
				break
			}
		}
		if target == nil {
			return respondEphemeral("Inbound connection %q is not linked to this team.", connName)
		}
	}

	remoteTeamName := ""
	if len(parts) >= 4 {
		remoteTeamName = parts[3]
	}

	fullConns, err := p.kvstore.GetTeamConnections(args.TeamId)
	if err != nil {
		return respondEphemeral("Failed to get team connections.")
	}

	var fullTarget *store.TeamConnection
	for i, tc := range fullConns {
		if tc.Direction == "inbound" && tc.Connection == target.Connection {
			fullTarget = &fullConns[i]
			break
		}
	}
	if fullTarget == nil {
		return respondEphemeral("Connection not found in team list.")
	}

	oldRemote := fullTarget.RemoteTeamName

	if remoteTeamName != "" {
		if err := p.kvstore.SetTeamRewriteIndex(target.Connection, remoteTeamName, args.TeamId); err != nil {
			return respondEphemeral("%s", err.Error())
		}
		fullTarget.RemoteTeamName = remoteTeamName
		if err := p.kvstore.SetTeamConnections(args.TeamId, fullConns); err != nil {
			return respondEphemeral("Failed to update team connections.")
		}
		if oldRemote != "" && oldRemote != remoteTeamName {
			_ = p.kvstore.DeleteTeamRewriteIndex(target.Connection, oldRemote)
		}
	} else {
		fullTarget.RemoteTeamName = ""
		if err := p.kvstore.SetTeamConnections(args.TeamId, fullConns); err != nil {
			return respondEphemeral("Failed to update team connections.")
		}
		if oldRemote != "" {
			_ = p.kvstore.DeleteTeamRewriteIndex(target.Connection, oldRemote)
		}
	}

	user, appErr := p.getUser(args.UserId)
	if appErr != nil {
		return respondEphemeral("Rewrite updated but failed to post audit message.")
	}

	channel, appErr := p.API.GetChannelByName(args.TeamId, "town-square", false)
	if appErr == nil {
		var msg string
		if remoteTeamName != "" {
			msg = fmt.Sprintf("Cross Guard rewrite set by @%s: inbound connection `%s` will route remote team name `%s` to this team.", user.Username, target.Connection, remoteTeamName)
		} else {
			msg = fmt.Sprintf("Cross Guard rewrite cleared by @%s for inbound connection `%s`.", user.Username, target.Connection)
		}
		post := &model.Post{
			UserId:    p.botUserID,
			ChannelId: channel.Id,
			Message:   msg,
		}
		_, _ = p.API.CreatePost(post)
	}

	if remoteTeamName != "" {
		return respondEphemeral("Rewrite set: inbound messages from `%s` with team name `%s` will route to this team.", target.Connection, remoteTeamName)
	}
	return respondEphemeral("Rewrite cleared for connection `%s`.", target.Connection)
}

func (p *Plugin) executeResetPrompt(args *model.CommandArgs) *model.CommandResponse {
	if !p.isTeamAdminOrSystemAdmin(args.UserId, args.TeamId) {
		return respondEphemeral("You must be a team admin or system admin to reset connection prompts.")
	}

	parts := strings.Fields(args.Command)
	if len(parts) < 3 {
		return respondEphemeral("Usage: /%s reset-prompt <connection-name>", commandTrigger)
	}
	connName := parts[2]

	prompt, err := p.kvstore.GetConnectionPrompt(args.TeamId, connName)
	if err != nil {
		return respondEphemeral("Failed to check connection prompt.")
	}
	if prompt == nil {
		return respondEphemeral("No prompt found for connection `%s` on this team.", connName)
	}

	if err := p.kvstore.DeleteConnectionPrompt(args.TeamId, connName); err != nil {
		return respondEphemeral("Failed to clear connection prompt.")
	}

	return respondEphemeral("Connection prompt for `%s` cleared. A new prompt will appear on the next inbound message.", connName)
}

func (p *Plugin) executeResetChannelPrompt(args *model.CommandArgs) *model.CommandResponse {
	if !p.isTeamAdminOrSystemAdmin(args.UserId, args.TeamId) {
		return respondEphemeral("You must be a team admin or system admin to reset channel connection prompts.")
	}

	parts := strings.Fields(args.Command)
	if len(parts) < 3 {
		return respondEphemeral("Usage: /%s reset-channel-prompt <connection-name>", commandTrigger)
	}
	connName := parts[2]

	prompt, err := p.kvstore.GetChannelConnectionPrompt(args.ChannelId, connName)
	if err != nil {
		return respondEphemeral("Failed to check channel connection prompt.")
	}
	if prompt == nil {
		return respondEphemeral("No channel prompt found for connection `%s` on this channel.", connName)
	}

	if err := p.kvstore.DeleteChannelConnectionPrompt(args.ChannelId, connName); err != nil {
		return respondEphemeral("Failed to clear channel connection prompt.")
	}

	return respondEphemeral("Channel connection prompt for `%s` cleared. A new prompt will appear on the next inbound message.", connName)
}

func (p *Plugin) isChannelAdminOrHigher(userID, channelID, teamID string) bool {
	if p.getConfiguration().isRestrictedToSystemAdmins() {
		return p.isTeamAdminOrSystemAdmin(userID, teamID)
	}

	member, appErr := p.API.GetChannelMember(channelID, userID)
	if appErr != nil || member == nil {
		return false
	}

	if member.SchemeAdmin {
		return true
	}

	return p.isTeamAdminOrSystemAdmin(userID, teamID)
}

func (p *Plugin) openConnectionDialog(triggerID, targetID string, connections []store.TeamConnection, action string) {
	var title string
	switch action {
	case actionInitTeam:
		title = "Link Connection to Team"
	case actionTeardownTeam:
		title = "Unlink Connection from Team"
	case actionInitChannel:
		title = "Link Connection to Channel"
	case actionTeardownChannel:
		title = "Unlink Connection from Channel"
	}

	options := make([]*model.PostActionOptions, 0, len(connections))
	for _, tc := range connections {
		key := connKey(tc)
		options = append(options, &model.PostActionOptions{
			Text:  key,
			Value: key,
		})
	}

	dialog := model.OpenDialogRequest{
		TriggerId: triggerID,
		URL:       fmt.Sprintf("/plugins/%s/api/v1/dialog/select-connection", manifest.Id),
		Dialog: model.Dialog{
			CallbackId:     "select_connection",
			Title:          title,
			SubmitLabel:    "Confirm",
			NotifyOnCancel: false,
			Elements: []model.DialogElement{
				{
					DisplayName: "Connection",
					Name:        "connection_name",
					Type:        "select",
					Options:     options,
					Placeholder: "Choose a connection...",
				},
			},
			State: action + ":" + targetID,
		},
	}

	if appErr := p.API.OpenInteractiveDialog(dialog); appErr != nil {
		p.API.LogError("Failed to open connection dialog",
			"error_code", errcode.CommandOpenConnDialogFailed,
			"error", appErr.Error())
	}
}

func respondEphemeral(format string, a ...any) *model.CommandResponse {
	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         fmt.Sprintf(format, a...),
	}
}
