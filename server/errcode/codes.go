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
	HooksChannelConnCheckFailed   = 10000
	HooksGetChannelFailed         = 10001
	HooksTeamConnCheckFailed      = 10002
	HooksGetTeamFailed            = 10003
	HooksRelaySemaphoreFull       = 10004
	HooksGetUserForPostFailed     = 10005
	HooksGetUserForUpdateFailed   = 10006
	HooksDeleteFlagCheckFailed    = 10007
	HooksGetPostForReactAddFailed = 10008
	HooksGetUserForReactAddFailed = 10009
	HooksGetPostForReactRemFailed = 10010
	HooksGetUserForReactRemFailed = 10011
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
)

// inbound.go (15000-15999)
const (
	InboundParseConnsFailed             = 15000
	InboundConnectFailed                = 15001
	InboundSubscribeFailed              = 15002
	InboundSubscriptionEstablished      = 15003
	InboundRelaySemaphoreFull           = 15004
	InboundUnmarshalFailed              = 15005
	InboundPostMissingPayload           = 15006
	InboundUpdateMissingPayload         = 15007
	InboundDeleteMissingPayload         = 15008
	InboundReactionAddMissingPayload    = 15009
	InboundReactionRemoveMissingPayload = 15010
	InboundTestMessageReceivedWithID    = 15011
	InboundTestMessageReceived          = 15012
	InboundUnknownMessageType           = 15013
	InboundMissingMessageQueueFull      = 15014
	InboundMissingMessageQueued         = 15015
	InboundPostResolveFailed            = 15016
	InboundPostIdempotencyLookupFailed  = 15017
	InboundPostResolveUserFailed        = 15018
	InboundPostRootMappingLookupFailed  = 15019
	InboundPostRootNotFoundStandalone   = 15020
	InboundPostCreateFailed             = 15021
	InboundPostStoreMappingFailed       = 15022
	InboundUpdateMappingLookupFailed    = 15023
	InboundUpdateGetLocalPostFailed     = 15024
	InboundUpdatePostFailed             = 15025
	InboundDeleteMappingLookupFailed    = 15026
	InboundDeleteSetFlagFailed          = 15027
	InboundDeletePostFailed             = 15028
	InboundDeleteRemoveFlagFailed       = 15029
	InboundDeleteRemoveMappingFailed    = 15030
	InboundReactionMappingLookupFailed  = 15031
	InboundReactionResolveFailed        = 15032
	InboundReactionResolveUserFailed    = 15033
	InboundReactionAddFailed            = 15034
	InboundReactionRemoveFailed         = 15035
	InboundFileWatcherStarted           = 15036
	InboundFileWatcherExited            = 15037
	InboundFileMissingHeaders           = 15038
	InboundFileConnInactive             = 15039
	InboundFileFilteredByPolicy         = 15040
	InboundFileMappingLookupFailed      = 15041
	InboundFileNoMappingFound           = 15042
	InboundFileGetLocalPostFailed       = 15043
	InboundFileUploadFailed             = 15044
	InboundFileAttachFailed             = 15045
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
	ConnectionsParseOutboundFailed     = 16000
	ConnectionsConnectOutboundFailed   = 16001
	ConnectionsOutboundEstablished     = 16002
	ConnectionsSerializeFailed         = 16003
	ConnectionsMessageSplit            = 16004
	ConnectionsSerializePartFailed     = 16005
	ConnectionsPublishPartFailed       = 16006
	ConnectionsPublishAfterRetriesFail = 16007
	ConnectionsGetFileInfoFailed       = 16008
	ConnectionsSkipOversizedFile       = 16009
	ConnectionsDownloadFileFailed      = 16010
	ConnectionsOutboundFileFiltered    = 16011
	ConnectionsFileSemaphoreFull       = 16012
	ConnectionsUploadFileFailed        = 16013
)

// sync_user.go (17000-17999)
const (
	SyncUserTruncatedUsername  = 17000
	SyncUserLookupFallback     = 17001
	SyncUserAddToTeamFailed    = 17002
	SyncUserAddToChannelFailed = 17003
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

// retry_dispatch.go (22000-22999)
const (
	RetryDispatchSucceeded      = 22000
	RetryDispatchStillMissing   = 22001
	RetryDispatchDropMaxAge     = 22002
	RetryDispatchDropUnmarshal  = 22003
	RetryDispatchDropMaxRetries = 22004
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

// AllCodes lists every code declared in this package. Used by
// TestCodesUnique to assert that no two call sites share a value.
// Keep in sync when adding new constants.
var AllCodes = []int{
	HooksChannelConnCheckFailed,
	HooksGetChannelFailed,
	HooksTeamConnCheckFailed,
	HooksGetTeamFailed,
	HooksRelaySemaphoreFull,
	HooksGetUserForPostFailed,
	HooksGetUserForUpdateFailed,
	HooksDeleteFlagCheckFailed,
	HooksGetPostForReactAddFailed,
	HooksGetUserForReactAddFailed,
	HooksGetPostForReactRemFailed,
	HooksGetUserForReactRemFailed,

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

	InboundParseConnsFailed,
	InboundConnectFailed,
	InboundSubscribeFailed,
	InboundSubscriptionEstablished,
	InboundRelaySemaphoreFull,
	InboundUnmarshalFailed,
	InboundPostMissingPayload,
	InboundUpdateMissingPayload,
	InboundDeleteMissingPayload,
	InboundReactionAddMissingPayload,
	InboundReactionRemoveMissingPayload,
	InboundTestMessageReceivedWithID,
	InboundTestMessageReceived,
	InboundUnknownMessageType,
	InboundMissingMessageQueueFull,
	InboundMissingMessageQueued,
	InboundPostResolveFailed,
	InboundPostIdempotencyLookupFailed,
	InboundPostResolveUserFailed,
	InboundPostRootMappingLookupFailed,
	InboundPostRootNotFoundStandalone,
	InboundPostCreateFailed,
	InboundPostStoreMappingFailed,
	InboundUpdateMappingLookupFailed,
	InboundUpdateGetLocalPostFailed,
	InboundUpdatePostFailed,
	InboundDeleteMappingLookupFailed,
	InboundDeleteSetFlagFailed,
	InboundDeletePostFailed,
	InboundDeleteRemoveFlagFailed,
	InboundDeleteRemoveMappingFailed,
	InboundReactionMappingLookupFailed,
	InboundReactionResolveFailed,
	InboundReactionResolveUserFailed,
	InboundReactionAddFailed,
	InboundReactionRemoveFailed,
	InboundFileWatcherStarted,
	InboundFileWatcherExited,
	InboundFileMissingHeaders,
	InboundFileConnInactive,
	InboundFileFilteredByPolicy,
	InboundFileMappingLookupFailed,
	InboundFileNoMappingFound,
	InboundFileGetLocalPostFailed,
	InboundFileUploadFailed,
	InboundFileAttachFailed,

	ConnectionsParseOutboundFailed,
	ConnectionsConnectOutboundFailed,
	ConnectionsOutboundEstablished,
	ConnectionsSerializeFailed,
	ConnectionsMessageSplit,
	ConnectionsSerializePartFailed,
	ConnectionsPublishPartFailed,
	ConnectionsPublishAfterRetriesFail,
	ConnectionsGetFileInfoFailed,
	ConnectionsSkipOversizedFile,
	ConnectionsDownloadFileFailed,
	ConnectionsOutboundFileFiltered,
	ConnectionsFileSemaphoreFull,
	ConnectionsUploadFileFailed,

	SyncUserTruncatedUsername,
	SyncUserLookupFallback,
	SyncUserAddToTeamFailed,
	SyncUserAddToChannelFailed,

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

	RetryDispatchSucceeded,
	RetryDispatchStillMissing,
	RetryDispatchDropMaxAge,
	RetryDispatchDropUnmarshal,
	RetryDispatchDropMaxRetries,

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
}
