// Package errcode defines distinct numeric identifiers for every
// p.API.Log* call in the Crossguard plugin. Each constant maps to exactly
// one call site so operators can grep production logs for a stable integer.
//
// Ranges are allocated by source file; see
// implementation-plans/26-04-11-01-error-codes-for-log-calls.md for the
// allocation table. When adding a new Log call, append the next unused
// code in that file's block and add the constant to AllCodes below.
package errcode

// hooks.go (10000-10999)
const (
	OutboundSyncMsgNoRemoteMatch       = 10100
	OutboundSyncMsgChannelLookupFailed = 10101
	OutboundSyncMsgPublishFailed       = 10102
	OutboundAttachmentStubbed          = 10103
	OutboundProfileImageStubbed        = 10104
	PingNoRemoteMatch                  = 10105
	PingOutboundUnhealthy              = 10106
	PingInboundUnhealthy               = 10107
	OutboundSyncMsgUserLookupFailed    = 10108
	OutboundSyncMsgUsersAugmented      = 10109

	OutboundAttachmentNoOutboundProvider  = 10110
	OutboundAttachmentConfigParseFailed   = 10111
	OutboundAttachmentDisabled            = 10112
	OutboundAttachmentFiltered            = 10113
	OutboundAttachmentSizeExceeded        = 10114
	OutboundAttachmentFileFetchFailed     = 10115
	OutboundAttachmentChannelLookupFailed = 10116
	OutboundAttachmentTeamLookupFailed    = 10117
	OutboundAttachmentFileInfoEncodeFail  = 10118
	OutboundAttachmentUploadFailed        = 10119

	OutboundProfileImageNoOutboundProvider = 10130
	OutboundProfileImageConfigParseFailed  = 10131
	OutboundProfileImageDisabled           = 10132
	OutboundProfileImageFetchFailed        = 10133
	OutboundProfileImageUploadFailed       = 10134
)

// configuration.go (12000-12999)
const (
	ConfigSameConfigPassed = 12000
	ConfigValidationWarn   = 12001
)

// command.go (13000-13999)
const (
	CommandOpenConnDialogFailed = 13000
)

// service.go (14000-14999)
const (
	ServiceInitTeamGetConnsFailed        = 14000
	ServiceAddTeamConnFailed             = 14001
	ServiceInitTeamReReadConnsFailed     = 14002
	ServiceAddTeamInitializedFailed      = 14003
	ServicePostTeamInitMsgFailed         = 14004
	ServiceCheckTeamStatusFailed         = 14005
	ServiceGetInitializedTeamsFailed     = 14006
	ServiceTeamStatusLookupTeamFailed    = 14007
	ServiceTeamStatusGetConnsFailed      = 14008
	ServiceParseOutboundConnFailed       = 14009
	ServiceParseInboundConnFailed        = 14010
	ServiceTeardownGetChanConnsFailed    = 14011
	ServiceTeardownGetTeamConnsFailed    = 14012
	ServiceInitChanGetTeamConnsFailed    = 14013
	ServiceInitChanGetChanConnsFailed    = 14014
	ServiceAddChanConnFailed             = 14015
	ServiceInitChanReReadConnsFailed     = 14016
	ServiceChanHeaderPrefixFailed        = 14017
	ServicePostChanInitMsgFailed         = 14018
	ServiceRemoveChanGetConnsFailed      = 14019
	ServiceRemoveChanConnFailed          = 14020
	ServiceRemoveChanReReadConnsFailed   = 14021
	ServiceDeleteChanConnsFailed         = 14022
	ServiceChanHeaderRemovePrefixFailed  = 14023
	ServicePostChanTeardownMsgFailed     = 14024
	ServiceTeardownTeamGetConnsFailed    = 14025
	ServiceRemoveTeamConnFailed          = 14026
	ServiceTeardownTeamReReadConnsFailed = 14027
	ServiceRemoveTeamInitializedFailed   = 14028
	ServicePostTeamTeardownMsgFailed     = 14029
	ServiceGlobalParseOutConnFailed      = 14030
	ServiceGlobalParseInConnFailed       = 14031
	ServiceMapParseOutConnFailed         = 14032
	ServiceMapParseInConnFailed          = 14033

	ServiceShareChannelFailed     = 14100
	ServiceInviteRemoteFailed     = 14101
	ServiceUninviteRemoteFailed   = 14102
	ServiceUnshareFailed          = 14103
	ServiceShareForRemoteRollback = 14104
)

// inbound.go (15000-15999)
const (
	InboundParseConnsFailed        = 15000
	InboundConnectFailed           = 15001
	InboundSubscribeFailed         = 15002
	InboundSubscriptionEstablished = 15003
	InboundUnmarshalFailed         = 15005

	InboundReceiveSyncFailed   = 15100
	InboundPostSyncErrors      = 15101
	InboundAttachmentStubbed   = 15102
	InboundProfileImageStubbed = 15103
	InboundUnknownType         = 15104
	InboundNoRemoteForConn     = 15105
	InboundNotConfigured       = 15106
	InboundTestReceived        = 15107
)

// api.go (11000-11999)
const (
	APINATSTestConnectFailed        = 11000
	APIBuildTestMessageFailed       = 11001
	APIPublishTestMessageFailed     = 11002
	APIFlushTestMessageFailed       = 11003
	APITestMessageSent              = 11004
	APISubscribeInboundTestFailed   = 11005
	APIFlushNATSConnFailed          = 11006
	APIInboundTestSubscribeOK       = 11007
	APIAzureQueueTestFailed         = 11008
	APIAzureBlobTestFailed          = 11009
	APIGetUserFailed                = 11010
	APIInitChannelGetTeamConns      = 11011
	APITeardownChannelGetChanConns  = 11012
	APITeardownTeamGetTeamConns     = 11013
	APIBulkChannelConnectionsFailed = 11014
	APITeamRewriteSet               = 11015
	APITeamRewriteCleared           = 11016
)

// connections.go (16000-16999)
const (
	ConnectionsParseOutboundFailed   = 16000
	ConnectionsConnectOutboundFailed = 16001
	ConnectionsOutboundEstablished   = 16002
	ConnectionsMessageSplit          = 16004
	ConnectionsSerializePartFailed   = 16005
	ConnectionsPublishPartFailed     = 16006
)

// azure_blob_provider.go (18000-18999)
const (
	AzureBlobContainerCreateRetry          = 18000
	AzureBlobWALInTempStorage              = 18001
	AzureBlobWALDirFsyncFailed             = 18002
	AzureBlobMarshalPendingFailed          = 18003
	AzureBlobWriteCompanionFailed          = 18004
	AzureBlobShutdownLeftWALFiles          = 18005
	AzureBlobWALFsyncRotationFailed        = 18006
	AzureBlobWALCloseRotationFailed        = 18007
	AzureBlobWALUploadFailed               = 18008
	AzureBlobDeleteWALFailed               = 18009
	AzureBlobMarshalFailedRefsFailed       = 18010
	AzureBlobRewriteCompanionFailed        = 18011
	AzureBlobDeleteCompanionFailed         = 18012
	AzureBlobDeferredFileFetchFailed       = 18013
	AzureBlobDeferredFileUploadFailed      = 18014
	AzureBlobShutdownMarshalResidualFailed = 18015
	AzureBlobShutdownPersistResidualFailed = 18016
	AzureBlobShutdownPersistedResidual     = 18017
	AzureBlobListFailed                    = 18018
	AzureBlobDeleteRetryFailed             = 18019
	AzureBlobDownloadFailed                = 18020
	AzureBlobHandlerError                  = 18021
	AzureBlobDeleteFailed                  = 18022
	AzureBlobWriteProcessedMarkerFailed    = 18023
	AzureBlobClearProcessedMarkerFailed    = 18024
	AzureBlobLockGetFailed                 = 18025
	AzureBlobLockTokenGenFailed            = 18026
	AzureBlobLockSetFailed                 = 18027
	AzureBlobCorruptLockReclaimFailed      = 18028
	AzureBlobStaleLockReclaimFailed        = 18029
	AzureBlobLockReleaseFailed             = 18030
	AzureBlobLockReleaseGetFailed          = 18031
	AzureBlobLockReleaseCorrupt            = 18032
	AzureBlobLockReleaseReclaimedByOther   = 18033
	AzureBlobLockReleaseConditionalFailed  = 18034
	AzureBlobWALRecoveryScanRootFailed     = 18035
	AzureBlobWALRecoveryScanDirFailed      = 18036
	AzureBlobWALRecoverySkipUnrecognized   = 18037
	AzureBlobWALRecoveryUploadFailed       = 18038
	AzureBlobWALRecoveryDeleteFailed       = 18039
	AzureBlobWALRecoveryReadCompanionFail  = 18040
	AzureBlobWALRecoveryMalformedCompanion = 18041
	AzureBlobWALRecoveryMarshalRemaining   = 18042
	AzureBlobWALRecoveryRewriteCompanion   = 18043
	AzureBlobFileListFailed                = 18044
	AzureBlobFileDeleteRetryFailed         = 18045
	AzureBlobFileDownloadFailed            = 18046
	AzureBlobFileHandlerError              = 18047
	AzureBlobFileDeleteFailed              = 18048
)

// azure_provider.go (19000-19999)
const (
	AzureQueueCreateQueueFailed   = 19000
	AzureQueueCreateContainerFail = 19001
	AzureQueueDequeueFailed       = 19002
	AzureQueueDecodeFailed        = 19003
	AzureQueueHandlerRetry        = 19004
	AzureQueueDeleteProcessedFail = 19005
	AzureQueueBlobListFailed      = 19006
	AzureQueueBlobDownloadFailed  = 19007
	AzureQueueBlobHandlerError    = 19008
	AzureQueueBlobDeleteFailed    = 19009
)

// nats_provider.go (20000-20999)
const (
	NATSDownloadFileFailed = 20000
	NATSFileHandlerError   = 20001
	NATSDisconnected       = 20002
	NATSReconnected        = 20003
	NATSDeleteFileFailed   = 20004
)

// prompt.go (21000-21999)
const (
	PromptGetConnPromptFailed    = 21000
	PromptGetTownSquareFailed    = 21001
	PromptCreatePostFailed       = 21002
	PromptSavePromptFailed       = 21003
	PromptAcceptGetPromptFailed  = 21004
	PromptDeletePromptFailed     = 21005
	PromptBlockGetPromptFailed   = 21006
	PromptSetBlockedFailed       = 21007
	PromptGetChanPromptFailed    = 21008
	PromptCreateChanPostFailed   = 21009
	PromptSaveChanPromptFailed   = 21010
	PromptChanAcceptGetFailed    = 21011
	PromptDeleteChanPromptFailed = 21012
	PromptChanBlockGetFailed     = 21013
	PromptSetChanBlockedFailed   = 21014
	PromptGetPostForUpdateFailed = 21015
	PromptUpdatePostFailed       = 21016
)

// store/caching.go (23000-23999)
const (
	StoreCachePublishInvalidationFailed = 23000
)

// request.go (24000-24999)
const (
	RequestGetFailed             = 24000
	RequestGetDMChannelFailed    = 24002
	RequestCreateDMPostFailed    = 24003
	RequestSaveFailed            = 24004
	RequestApproveGetFailed      = 24005
	RequestApproveExecFailed     = 24006
	RequestApproveDeleteFailed   = 24007
	RequestApproveNotifyFailed   = 24008
	RequestDenyDialogFailed      = 24009
	RequestDenyGetFailed         = 24010
	RequestDenyDeleteFailed      = 24011
	RequestDenyNotifyFailed      = 24012
	RequestUpdatePostFailed      = 24013
	RequestNoSystemAdmins        = 24014
	RequestConnRemovedFromConfig = 24015
	RequestConfirmDMFailed       = 24016
	RequestNoAdminsNotified      = 24017
)

// channel_request.go (25000-25999)
const (
	ChanRequestGetFailed             = 25000
	ChanRequestGetDMChannelFailed    = 25002
	ChanRequestCreateDMPostFailed    = 25003
	ChanRequestSaveFailed            = 25004
	ChanRequestApproveGetFailed      = 25005
	ChanRequestApproveExecFailed     = 25006
	ChanRequestApproveDeleteFailed   = 25007
	ChanRequestApproveNotifyFailed   = 25008
	ChanRequestDenyDialogFailed      = 25009
	ChanRequestDenyGetFailed         = 25010
	ChanRequestDenyDeleteFailed      = 25011
	ChanRequestDenyNotifyFailed      = 25012
	ChanRequestUpdatePostFailed      = 25013
	ChanRequestNoTeamAdmins          = 25014
	ChanRequestConnRemovedFromConfig = 25015
	ChanRequestConfirmDMFailed       = 25016
	ChanRequestNoAdminsNotified      = 25017
	ChanRequestGetTeamAdminsFailed   = 25018
)

// Azure Service Bus provider: 26000-26999.
const (
	ServiceBusSendFailed         = 26000
	ServiceBusReceiveFailed      = 26001
	ServiceBusCompleteFailed     = 26002
	ServiceBusAbandonFailed      = 26003
	ServiceBusRedelivery         = 26004
	ServiceBusMalformedBody      = 26005
	APIAzureServiceBusTestFailed = 26006
)

// plugin.go (27000-27999)
const (
	PluginRegisterFailed   = 27000
	PluginUnregisterFailed = 27001

	// Upgrade migrations (one-shot, run from OnActivate).
	PluginMigrateListKVFailed     = 27100
	PluginMigrateDeleteKVFailed   = 27101
	PluginMigrateRecordKVFailed   = 27102
	PluginMigrateGetChannelFailed = 27103
	PluginMigrateShareFailed      = 27104
	PluginMigrateInviteFailed     = 27105
	PluginMigrateSummary          = 27106
	PluginMigrateDeferred         = 27107
)

// inbound_files.go (28000-28999)
const (
	InboundFileUnknownKind                = 28000
	InboundAttachmentMissingHeader        = 28001
	InboundAttachmentFileInfoDecodeFailed = 28002
	InboundAttachmentTeamLookupFailed     = 28003
	InboundAttachmentChannelLookupFailed  = 28004
	InboundAttachmentChannelUnlinked      = 28005
	InboundAttachmentNoRemoteForConn      = 28006
	InboundAttachmentReceiveFailed        = 28007
	InboundProfileImageMissingHeader      = 28008
	InboundProfileImageNoRemoteForConn    = 28009
	InboundProfileImageReceiveFailed      = 28010
	InboundFileWatcherRestart             = 28011
	InboundAttachmentDisabled             = 28012
	InboundAttachmentFiltered             = 28013
	InboundProfileImageDisabled           = 28014
)

// AllCodes lists every code declared in this package. Used by
// TestCodesUnique to assert that no two call sites share a value.
// Keep in sync when adding new constants.
var AllCodes = []int{
	OutboundSyncMsgNoRemoteMatch,
	OutboundSyncMsgChannelLookupFailed,
	OutboundSyncMsgPublishFailed,
	OutboundAttachmentStubbed,
	OutboundProfileImageStubbed,
	PingNoRemoteMatch,
	PingOutboundUnhealthy,
	PingInboundUnhealthy,
	OutboundSyncMsgUserLookupFailed,
	OutboundSyncMsgUsersAugmented,
	OutboundAttachmentNoOutboundProvider,
	OutboundAttachmentConfigParseFailed,
	OutboundAttachmentDisabled,
	OutboundAttachmentFiltered,
	OutboundAttachmentSizeExceeded,
	OutboundAttachmentFileFetchFailed,
	OutboundAttachmentChannelLookupFailed,
	OutboundAttachmentTeamLookupFailed,
	OutboundAttachmentFileInfoEncodeFail,
	OutboundAttachmentUploadFailed,
	OutboundProfileImageNoOutboundProvider,
	OutboundProfileImageConfigParseFailed,
	OutboundProfileImageDisabled,
	OutboundProfileImageFetchFailed,
	OutboundProfileImageUploadFailed,

	APINATSTestConnectFailed,
	APIBuildTestMessageFailed,
	APIPublishTestMessageFailed,
	APIFlushTestMessageFailed,
	APITestMessageSent,
	APISubscribeInboundTestFailed,
	APIFlushNATSConnFailed,
	APIInboundTestSubscribeOK,
	APIAzureQueueTestFailed,
	APIAzureBlobTestFailed,
	APIGetUserFailed,
	APIInitChannelGetTeamConns,
	APITeardownChannelGetChanConns,
	APITeardownTeamGetTeamConns,
	APIBulkChannelConnectionsFailed,
	APITeamRewriteSet,
	APITeamRewriteCleared,

	ConfigSameConfigPassed,
	ConfigValidationWarn,

	CommandOpenConnDialogFailed,

	ServiceInitTeamGetConnsFailed,
	ServiceAddTeamConnFailed,
	ServiceInitTeamReReadConnsFailed,
	ServiceAddTeamInitializedFailed,
	ServicePostTeamInitMsgFailed,
	ServiceCheckTeamStatusFailed,
	ServiceGetInitializedTeamsFailed,
	ServiceTeamStatusLookupTeamFailed,
	ServiceTeamStatusGetConnsFailed,
	ServiceParseOutboundConnFailed,
	ServiceParseInboundConnFailed,
	ServiceTeardownGetChanConnsFailed,
	ServiceTeardownGetTeamConnsFailed,
	ServiceInitChanGetTeamConnsFailed,
	ServiceInitChanGetChanConnsFailed,
	ServiceAddChanConnFailed,
	ServiceInitChanReReadConnsFailed,
	ServiceChanHeaderPrefixFailed,
	ServicePostChanInitMsgFailed,
	ServiceRemoveChanGetConnsFailed,
	ServiceRemoveChanConnFailed,
	ServiceRemoveChanReReadConnsFailed,
	ServiceDeleteChanConnsFailed,
	ServiceChanHeaderRemovePrefixFailed,
	ServicePostChanTeardownMsgFailed,
	ServiceTeardownTeamGetConnsFailed,
	ServiceRemoveTeamConnFailed,
	ServiceTeardownTeamReReadConnsFailed,
	ServiceRemoveTeamInitializedFailed,
	ServicePostTeamTeardownMsgFailed,
	ServiceGlobalParseOutConnFailed,
	ServiceGlobalParseInConnFailed,
	ServiceMapParseOutConnFailed,
	ServiceMapParseInConnFailed,
	ServiceShareChannelFailed,
	ServiceInviteRemoteFailed,
	ServiceUninviteRemoteFailed,
	ServiceUnshareFailed,
	ServiceShareForRemoteRollback,

	InboundParseConnsFailed,
	InboundConnectFailed,
	InboundSubscribeFailed,
	InboundSubscriptionEstablished,
	InboundUnmarshalFailed,
	InboundReceiveSyncFailed,
	InboundPostSyncErrors,
	InboundAttachmentStubbed,
	InboundProfileImageStubbed,
	InboundUnknownType,
	InboundNoRemoteForConn,
	InboundNotConfigured,
	InboundTestReceived,

	ConnectionsParseOutboundFailed,
	ConnectionsConnectOutboundFailed,
	ConnectionsOutboundEstablished,
	ConnectionsMessageSplit,
	ConnectionsSerializePartFailed,
	ConnectionsPublishPartFailed,

	AzureBlobContainerCreateRetry,
	AzureBlobWALInTempStorage,
	AzureBlobWALDirFsyncFailed,
	AzureBlobMarshalPendingFailed,
	AzureBlobWriteCompanionFailed,
	AzureBlobShutdownLeftWALFiles,
	AzureBlobWALFsyncRotationFailed,
	AzureBlobWALCloseRotationFailed,
	AzureBlobWALUploadFailed,
	AzureBlobDeleteWALFailed,
	AzureBlobMarshalFailedRefsFailed,
	AzureBlobRewriteCompanionFailed,
	AzureBlobDeleteCompanionFailed,
	AzureBlobDeferredFileFetchFailed,
	AzureBlobDeferredFileUploadFailed,
	AzureBlobShutdownMarshalResidualFailed,
	AzureBlobShutdownPersistResidualFailed,
	AzureBlobShutdownPersistedResidual,
	AzureBlobListFailed,
	AzureBlobDeleteRetryFailed,
	AzureBlobDownloadFailed,
	AzureBlobHandlerError,
	AzureBlobDeleteFailed,
	AzureBlobWriteProcessedMarkerFailed,
	AzureBlobClearProcessedMarkerFailed,
	AzureBlobLockGetFailed,
	AzureBlobLockTokenGenFailed,
	AzureBlobLockSetFailed,
	AzureBlobCorruptLockReclaimFailed,
	AzureBlobStaleLockReclaimFailed,
	AzureBlobLockReleaseFailed,
	AzureBlobLockReleaseGetFailed,
	AzureBlobLockReleaseCorrupt,
	AzureBlobLockReleaseReclaimedByOther,
	AzureBlobLockReleaseConditionalFailed,
	AzureBlobWALRecoveryScanRootFailed,
	AzureBlobWALRecoveryScanDirFailed,
	AzureBlobWALRecoverySkipUnrecognized,
	AzureBlobWALRecoveryUploadFailed,
	AzureBlobWALRecoveryDeleteFailed,
	AzureBlobWALRecoveryReadCompanionFail,
	AzureBlobWALRecoveryMalformedCompanion,
	AzureBlobWALRecoveryMarshalRemaining,
	AzureBlobWALRecoveryRewriteCompanion,
	AzureBlobFileListFailed,
	AzureBlobFileDeleteRetryFailed,
	AzureBlobFileDownloadFailed,
	AzureBlobFileHandlerError,
	AzureBlobFileDeleteFailed,

	AzureQueueCreateQueueFailed,
	AzureQueueCreateContainerFail,
	AzureQueueDequeueFailed,
	AzureQueueDecodeFailed,
	AzureQueueHandlerRetry,
	AzureQueueDeleteProcessedFail,
	AzureQueueBlobListFailed,
	AzureQueueBlobDownloadFailed,
	AzureQueueBlobHandlerError,
	AzureQueueBlobDeleteFailed,

	NATSDownloadFileFailed,
	NATSFileHandlerError,
	NATSDisconnected,
	NATSReconnected,
	NATSDeleteFileFailed,

	PromptGetConnPromptFailed,
	PromptGetTownSquareFailed,
	PromptCreatePostFailed,
	PromptSavePromptFailed,
	PromptAcceptGetPromptFailed,
	PromptDeletePromptFailed,
	PromptBlockGetPromptFailed,
	PromptSetBlockedFailed,
	PromptGetChanPromptFailed,
	PromptCreateChanPostFailed,
	PromptSaveChanPromptFailed,
	PromptChanAcceptGetFailed,
	PromptDeleteChanPromptFailed,
	PromptChanBlockGetFailed,
	PromptSetChanBlockedFailed,
	PromptGetPostForUpdateFailed,
	PromptUpdatePostFailed,

	StoreCachePublishInvalidationFailed,

	RequestGetFailed,
	RequestGetDMChannelFailed,
	RequestCreateDMPostFailed,
	RequestSaveFailed,
	RequestApproveGetFailed,
	RequestApproveExecFailed,
	RequestApproveDeleteFailed,
	RequestApproveNotifyFailed,
	RequestDenyDialogFailed,
	RequestDenyGetFailed,
	RequestDenyDeleteFailed,
	RequestDenyNotifyFailed,
	RequestUpdatePostFailed,
	RequestNoSystemAdmins,
	RequestConnRemovedFromConfig,
	RequestConfirmDMFailed,
	RequestNoAdminsNotified,

	ChanRequestGetFailed,
	ChanRequestGetDMChannelFailed,
	ChanRequestCreateDMPostFailed,
	ChanRequestSaveFailed,
	ChanRequestApproveGetFailed,
	ChanRequestApproveExecFailed,
	ChanRequestApproveDeleteFailed,
	ChanRequestApproveNotifyFailed,
	ChanRequestDenyDialogFailed,
	ChanRequestDenyGetFailed,
	ChanRequestDenyDeleteFailed,
	ChanRequestDenyNotifyFailed,
	ChanRequestUpdatePostFailed,
	ChanRequestNoTeamAdmins,
	ChanRequestConnRemovedFromConfig,
	ChanRequestConfirmDMFailed,
	ChanRequestNoAdminsNotified,
	ChanRequestGetTeamAdminsFailed,

	ServiceBusSendFailed,
	ServiceBusReceiveFailed,
	ServiceBusCompleteFailed,
	ServiceBusAbandonFailed,
	ServiceBusRedelivery,
	ServiceBusMalformedBody,
	APIAzureServiceBusTestFailed,

	PluginRegisterFailed,
	PluginUnregisterFailed,
	PluginMigrateListKVFailed,
	PluginMigrateDeleteKVFailed,
	PluginMigrateRecordKVFailed,
	PluginMigrateGetChannelFailed,
	PluginMigrateShareFailed,
	PluginMigrateInviteFailed,
	PluginMigrateSummary,
	PluginMigrateDeferred,

	InboundFileUnknownKind,
	InboundAttachmentMissingHeader,
	InboundAttachmentFileInfoDecodeFailed,
	InboundAttachmentTeamLookupFailed,
	InboundAttachmentChannelLookupFailed,
	InboundAttachmentChannelUnlinked,
	InboundAttachmentNoRemoteForConn,
	InboundAttachmentReceiveFailed,
	InboundProfileImageMissingHeader,
	InboundProfileImageNoRemoteForConn,
	InboundProfileImageReceiveFailed,
	InboundFileWatcherRestart,
	InboundAttachmentDisabled,
	InboundAttachmentFiltered,
	InboundProfileImageDisabled,
}
