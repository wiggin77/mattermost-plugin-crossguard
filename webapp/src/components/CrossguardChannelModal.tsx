import manifest from 'manifest';
import React from 'react';

import {providerLabel} from '../provider_label';

interface ConnectionStatus {
    name: string;
    direction: string;
    provider?: string;
    linked: boolean;
    orphaned?: boolean;
    remote_team_name?: string;
    file_transfer_enabled: boolean;
    file_filter_mode?: string;
    file_filter_types?: string;
    request_pending?: boolean;
}

interface ChannelStatusResponse {
    channel_id: string;
    channel_name: string;
    channel_display_name: string;
    team_name: string;
    team_connections: ConnectionStatus[];
    channel_request_mode?: boolean;
}

interface Status {
    loading: boolean;
    success?: boolean;
    message?: string;
}

function getCSRFToken(): string {
    const match = document.cookie.match(/MMCSRF=([^;]+)/);
    return match ? match[1] : '';
}

const colors = {
    primary: '#1C58D9',
    danger: '#D24B4E',
    border: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.16)',
    bg: 'var(--center-channel-bg, #fff)',
    text: 'var(--center-channel-color, #3D3C40)',
    textMuted: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.64)',
    textSubtle: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.48)',
    inbound: '#1B9AAA',
    outbound: '#5A4FCF',
};

const STATUS_DISPLAY_MS = 5000;

function renderLinkButtonLabel(isActioning: boolean, isRequestMode: boolean): string {
    if (isActioning) {
        return isRequestMode ? 'Requesting...' : 'Linking...';
    }
    return isRequestMode ? 'Request Link' : 'Link';
}

const CrossguardChannelModal: React.FC = () => {
    const [channelID, setChannelID] = React.useState<string | null>(null);
    const [teamName, setTeamName] = React.useState('');
    const [channelName, setChannelName] = React.useState('');
    const [teamConnections, setTeamConnections] = React.useState<ConnectionStatus[]>([]);
    const [fetching, setFetching] = React.useState(false);
    const [status, setStatus] = React.useState<Status>({loading: false});
    const [actionInProgress, setActionInProgress] = React.useState<string | null>(null);
    const [requestMode, setRequestMode] = React.useState(false);
    const statusTimerRef = React.useRef<ReturnType<typeof setTimeout> | null>(null);

    React.useEffect(() => {
        return () => {
            if (statusTimerRef.current) {
                clearTimeout(statusTimerRef.current);
            }
        };
    }, []);

    React.useEffect(() => {
        const handler = (e: Event) => {
            const detail = (e as CustomEvent).detail;
            if (detail?.channelID) {
                setChannelID(detail.channelID);
                setTeamName('');
                setChannelName('');
                setTeamConnections([]);
                setStatus({loading: false});
                setActionInProgress(null);
                setRequestMode(false);
            }
        };
        document.addEventListener('crossguard:open-modal', handler);
        return () => document.removeEventListener('crossguard:open-modal', handler);
    }, []);

    const fetchStatus = React.useCallback(async (chID: string) => {
        setFetching(true);
        try {
            const response = await fetch(`/plugins/${manifest.id}/api/v1/channels/${chID}/status`, {
                credentials: 'same-origin',
                headers: {'X-Requested-With': 'XMLHttpRequest'},
            });
            if (!response.ok) {
                const data = await response.json();
                setStatus({loading: false, success: false, message: data.error || 'Failed to load channel status.'});
                return;
            }
            const data: ChannelStatusResponse = await response.json();
            setTeamName(data.team_name);
            setChannelName(data.channel_display_name);
            setTeamConnections(data.team_connections || []);
            setRequestMode(data.channel_request_mode || false);
        } catch {
            setStatus({loading: false, success: false, message: 'Network error loading channel status.'});
        } finally {
            setFetching(false);
        }
    }, []);

    React.useEffect(() => {
        if (channelID) {
            fetchStatus(channelID);
        }
    }, [channelID, fetchStatus]);

    React.useEffect(() => {
        if (!channelID) {
            return undefined;
        }
        const handler = (e: KeyboardEvent) => {
            if (e.key === 'Escape') {
                setChannelID(null);
            }
        };
        document.addEventListener('keydown', handler);
        return () => document.removeEventListener('keydown', handler);
    }, [channelID]);

    const callAPI = React.useCallback(async (url: string): Promise<{ok: boolean; data: Record<string, unknown>}> => {
        const response = await fetch(url, {
            method: 'POST',
            credentials: 'same-origin',
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-Token': getCSRFToken(),
                'X-Requested-With': 'XMLHttpRequest',
            },
        });
        const data = await response.json();
        return {ok: response.ok, data};
    }, []);

    const handleToggle = React.useCallback(async (conn: ConnectionStatus) => {
        if (!channelID) {
            return;
        }
        const qualifiedName = `${conn.direction}:${conn.name}`;
        const action = conn.linked ? 'teardown' : 'init';
        const verb = conn.linked ? 'unlinked' : 'linked';
        const failVerb = conn.linked ? 'unlink' : 'link';
        setActionInProgress(qualifiedName);
        setStatus({loading: true});
        if (statusTimerRef.current) {
            clearTimeout(statusTimerRef.current);
        }
        try {
            const url = `/plugins/${manifest.id}/api/v1/channels/${channelID}/${action}?connection_name=${encodeURIComponent(qualifiedName)}`;
            const {ok, data} = await callAPI(url);
            if (ok) {
                if (data.status === 'request_submitted') {
                    setStatus({loading: false, success: true, message: (data.message as string) || 'Your request has been submitted for approval.'});
                } else {
                    setStatus({loading: false, success: true, message: `Connection "${conn.name}" ${verb}.`});
                }
            } else {
                setStatus({loading: false, success: false, message: (data.error as string) || `Failed to ${failVerb} connection.`});
            }
            await fetchStatus(channelID);
            statusTimerRef.current = setTimeout(() => setStatus({loading: false}), STATUS_DISPLAY_MS);
        } catch {
            setStatus({loading: false, success: false, message: 'Network error.'});
            statusTimerRef.current = setTimeout(() => setStatus({loading: false}), STATUS_DISPLAY_MS);
        } finally {
            setActionInProgress(null);
        }
    }, [callAPI, channelID, fetchStatus]);

    const handleBackdropClick = React.useCallback((e: React.MouseEvent) => {
        if (e.target === e.currentTarget) {
            setChannelID(null);
        }
    }, []);

    if (!channelID) {
        return null;
    }

    const s: Record<string, React.CSSProperties> = {
        backdrop: {
            position: 'fixed',
            top: 0,
            left: 0,
            right: 0,
            bottom: 0,
            background: 'rgba(0, 0, 0, 0.5)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            zIndex: 10000,
        },
        modal: {
            background: colors.bg,
            borderRadius: '8px',
            width: '560px',
            maxWidth: '90vw',
            boxShadow: '0 12px 32px rgba(0, 0, 0, 0.2)',
            color: colors.text,
        },
        header: {
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            padding: '20px 24px 16px',
            borderBottom: `1px solid ${colors.border}`,
        },
        title: {
            fontSize: '18px',
            fontWeight: 600,
            margin: 0,
        },
        closeBtn: {
            background: 'none',
            border: 'none',
            fontSize: '20px',
            cursor: 'pointer',
            color: colors.textMuted,
            padding: '4px 8px',
            lineHeight: 1,
        },
        body: {
            padding: '24px',
        },
        card: {
            border: `1px solid ${colors.border}`,
            borderRadius: '8px',
            padding: '14px 16px',
            marginBottom: '10px',
            display: 'flex',
            alignItems: 'center',
            gap: '12px',
        },
        cardAccent: {
            width: '4px',
            alignSelf: 'stretch',
            borderRadius: '2px',
            flexShrink: 0,
        },
        cardContent: {
            flex: 1,
            minWidth: 0,
        },
        cardTop: {
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            marginBottom: '4px',
        },
        connName: {
            fontSize: '15px',
            fontWeight: 600,
            color: colors.text,
        },
        directionBadge: {
            display: 'inline-flex',
            alignItems: 'center',
            padding: '2px 10px',
            borderRadius: '10px',
            fontSize: '11px',
            fontWeight: 600,
            lineHeight: '16px',
            textTransform: 'uppercase' as const,
            letterSpacing: '0.3px',
            whiteSpace: 'nowrap' as const,
        },
        directionLabel: {
            fontSize: '13px',
            color: colors.textMuted,
            lineHeight: '18px',
        },
        remoteTeamLabel: {
            fontSize: '12px',
            color: colors.textMuted,
            lineHeight: '16px',
        },
        btnLink: {
            padding: '6px 16px',
            cursor: 'pointer',
            border: 'none',
            borderRadius: '4px',
            background: colors.primary,
            color: '#fff',
            fontSize: '13px',
            fontWeight: 600,
            whiteSpace: 'nowrap' as const,
        },
        btnUnlink: {
            padding: '6px 16px',
            cursor: 'pointer',
            border: 'none',
            borderRadius: '4px',
            background: colors.danger,
            color: '#fff',
            fontSize: '13px',
            fontWeight: 600,
            whiteSpace: 'nowrap' as const,
        },
        statusBanner: {
            display: 'flex',
            alignItems: 'center',
            gap: '8px',
            padding: '8px 12px',
            borderRadius: '4px',
            fontSize: '13px',
            fontWeight: 500,
            marginTop: '16px',
        },
        statusSuccess: {
            background: 'rgba(61, 184, 135, 0.12)',
            color: '#1B8A5C',
        },
        statusError: {
            background: 'rgba(210, 75, 78, 0.12)',
            color: colors.danger,
        },
        emptyState: {
            fontSize: '14px',
            color: colors.textSubtle,
            textAlign: 'center' as const,
            padding: '24px 0',
        },
        helpLink: {
            marginTop: '16px',
            paddingTop: '12px',
            borderTop: `1px solid ${colors.border}`,
            textAlign: 'center' as const,
        },
        helpAnchor: {
            fontSize: '13px',
            color: colors.primary,
            textDecoration: 'none',
        },
    };

    const renderBody = () => {
        if (fetching) {
            return <p style={{...s.emptyState, color: colors.textMuted}}>{'Loading...'}</p>;
        }

        if (teamConnections.length === 0) {
            return (
                <p style={s.emptyState}>
                    {'No connections available. Configure connections in the System Console.'}
                </p>
            );
        }

        return (
            <div>
                {teamConnections.map((conn) => {
                    const direction = conn.direction;
                    const qualifiedName = `${conn.direction}:${conn.name}`;
                    const isInbound = direction === 'inbound';
                    const accentColor = isInbound ? colors.inbound : colors.outbound;
                    const isActioning = actionInProgress === qualifiedName;

                    const badgeStyle: React.CSSProperties = {
                        ...s.directionBadge,
                        background: accentColor + '1A',
                        color: accentColor,
                    };

                    const provLabel = providerLabel(conn.provider);
                    const badgeLabel = isInbound ? `${provLabel} \u2192 MATTERMOST` : `MATTERMOST \u2192 ${provLabel}`;

                    return (
                        <div
                            key={qualifiedName}
                            style={{
                                ...s.card,
                                background: accentColor + '08',
                                borderColor: accentColor + '30',
                            }}
                        >
                            <div style={{...s.cardAccent, background: accentColor}}/>
                            <div style={s.cardContent}>
                                <div style={s.cardTop}>
                                    <span style={s.connName}>{conn.name}</span>
                                    <span style={badgeStyle}>{badgeLabel}</span>
                                    {conn.orphaned && <span title={'Connection no longer in configuration'}>{'🔗\u200D💔'}</span>}
                                </div>
                                {conn.remote_team_name && (
                                    <div style={s.remoteTeamLabel}>
                                        {`Remote team: ${conn.remote_team_name}`}
                                    </div>
                                )}
                                <div style={s.directionLabel}>
                                    {conn.file_transfer_enabled ? (
                                        <>
                                            {'\u{1F4CE} Files: '}
                                            {conn.file_filter_mode === 'allow' && `Allow ${conn.file_filter_types}`}
                                            {conn.file_filter_mode === 'deny' && `Deny ${conn.file_filter_types}`}
                                            {!conn.file_filter_mode && 'All types'}
                                        </>
                                    ) : (
                                        '\u{1F6AB} Files: Disabled'
                                    )}
                                </div>
                            </div>
                            {conn.linked && (
                                <button
                                    style={s.btnUnlink}
                                    onClick={() => handleToggle(conn)}
                                    disabled={actionInProgress !== null}
                                >
                                    {isActioning ? 'Unlinking...' : 'Unlink'}
                                </button>
                            )}
                            {!conn.linked && conn.request_pending && (
                                <button
                                    style={{...s.btnLink, opacity: 0.5, cursor: 'default'}}
                                    disabled={true}
                                    title={'A connection request is awaiting team admin approval'}
                                >
                                    {'Request Pending'}
                                </button>
                            )}
                            {!conn.linked && !conn.request_pending && (
                                <button
                                    style={s.btnLink}
                                    onClick={() => handleToggle(conn)}
                                    disabled={actionInProgress !== null}
                                >
                                    {renderLinkButtonLabel(isActioning, requestMode)}
                                </button>
                            )}
                        </div>
                    );
                })}
            </div>
        );
    };

    const renderStatus = () => {
        if (status.loading || status.success === undefined) {
            return null;
        }
        const bannerStyle = {
            ...s.statusBanner,
            ...(status.success ? s.statusSuccess : s.statusError),
        };
        return (
            <div style={bannerStyle}>
                {status.message}
            </div>
        );
    };

    return (
        <div
            style={s.backdrop}
            onClick={handleBackdropClick}
        >
            <div style={s.modal}>
                <div style={s.header}>
                    <h2 style={s.title}>{`Cross Guard Settings for ${teamName || '...'} > ${channelName || '...'}`}</h2>
                    <button
                        style={s.closeBtn}
                        onClick={() => setChannelID(null)}
                    >
                        {'\u00D7'}
                    </button>
                </div>
                <div style={s.body}>
                    {renderBody()}
                    {renderStatus()}
                    <div style={s.helpLink}>
                        <a
                            href={`/plugins/${manifest.id}/public/help/help.html`}
                            target={'_blank'}
                            rel={'noopener noreferrer'}
                            style={s.helpAnchor}
                        >
                            {'View Cross Guard Documentation'}
                        </a>
                    </div>
                </div>
            </div>
        </div>
    );
};

export default CrossguardChannelModal;
