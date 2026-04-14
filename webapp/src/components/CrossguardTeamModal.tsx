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

interface TeamStatusResponse {
    team_id: string;
    team_name: string;
    team_display_name: string;
    initialized: boolean;
    connections: ConnectionStatus[];
    request_mode?: boolean;
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

const CrossguardTeamModal: React.FC = () => {
    const [teamID, setTeamID] = React.useState<string | null>(null);
    const [teamName, setTeamName] = React.useState('');
    const [connections, setConnections] = React.useState<ConnectionStatus[]>([]);
    const [fetching, setFetching] = React.useState(false);
    const [status, setStatus] = React.useState<Status>({loading: false});
    const [actionInProgress, setActionInProgress] = React.useState<string | null>(null);
    const [editingRewrite, setEditingRewrite] = React.useState<string | null>(null);
    const [rewriteInput, setRewriteInput] = React.useState('');
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
            if (detail?.teamID) {
                setTeamID(detail.teamID);
                setTeamName('');
                setConnections([]);
                setStatus({loading: false});
                setActionInProgress(null);
                setEditingRewrite(null);
                setRewriteInput('');
                setRequestMode(false);
            }
        };
        document.addEventListener('crossguard:open-team-modal', handler);
        return () => document.removeEventListener('crossguard:open-team-modal', handler);
    }, []);

    const fetchStatus = React.useCallback(async (tID: string) => {
        setFetching(true);
        try {
            const response = await fetch(`/plugins/${manifest.id}/api/v1/teams/${tID}/status`, {
                credentials: 'same-origin',
                headers: {'X-Requested-With': 'XMLHttpRequest'},
            });
            if (!response.ok) {
                const data = await response.json();
                setStatus({loading: false, success: false, message: data.error || 'Failed to load team status.'});
                return;
            }
            const data: TeamStatusResponse = await response.json();
            setTeamName(data.team_display_name);
            setConnections(data.connections || []);
            setRequestMode(data.request_mode || false);
        } catch {
            setStatus({loading: false, success: false, message: 'Network error loading team status.'});
        } finally {
            setFetching(false);
        }
    }, []);

    React.useEffect(() => {
        if (teamID) {
            fetchStatus(teamID);
        }
    }, [teamID, fetchStatus]);

    React.useEffect(() => {
        if (!teamID) {
            return undefined;
        }
        const handler = (e: KeyboardEvent) => {
            if (e.key === 'Escape') {
                setTeamID(null);
            }
        };
        document.addEventListener('keydown', handler);
        return () => document.removeEventListener('keydown', handler);
    }, [teamID]);

    const callAPI = React.useCallback(async (url: string, options?: RequestInit): Promise<{ok: boolean; data: Record<string, unknown>}> => {
        const response = await fetch(url, {
            method: 'POST',
            credentials: 'same-origin',
            headers: {
                'Content-Type': 'application/json',
                'X-CSRF-Token': getCSRFToken(),
                'X-Requested-With': 'XMLHttpRequest',
            },
            ...options,
        });
        const data = await response.json();
        return {ok: response.ok, data};
    }, []);

    const handleToggle = React.useCallback(async (conn: ConnectionStatus, linked: boolean) => {
        if (!teamID) {
            return;
        }
        const connKey = `${conn.direction}:${conn.name}`;
        const action = linked ? 'teardown' : 'init';
        const verb = linked ? 'unlinked' : 'linked';
        const failVerb = linked ? 'unlink' : 'link';
        setActionInProgress(connKey);
        setStatus({loading: true});
        if (statusTimerRef.current) {
            clearTimeout(statusTimerRef.current);
        }
        try {
            const url = `/plugins/${manifest.id}/api/v1/teams/${teamID}/${action}?connection_name=${encodeURIComponent(connKey)}`;
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
            await fetchStatus(teamID);
            statusTimerRef.current = setTimeout(() => setStatus({loading: false}), STATUS_DISPLAY_MS);
        } catch {
            setStatus({loading: false, success: false, message: 'Network error.'});
            statusTimerRef.current = setTimeout(() => setStatus({loading: false}), STATUS_DISPLAY_MS);
        } finally {
            setActionInProgress(null);
        }
    }, [callAPI, teamID, fetchStatus]);

    const handleSaveRewrite = React.useCallback(async (connName: string, value: string) => {
        if (!teamID) {
            return;
        }
        setStatus({loading: true});
        if (statusTimerRef.current) {
            clearTimeout(statusTimerRef.current);
        }
        try {
            const url = `/plugins/${manifest.id}/api/v1/teams/${teamID}/rewrite`;
            const {ok, data} = await callAPI(url, {
                body: JSON.stringify({connection: connName, remote_team_name: value}),
            });
            if (ok) {
                setStatus({loading: false, success: true, message: `Remote team rewrite updated for "${connName}".`});
            } else {
                setStatus({loading: false, success: false, message: (data.error as string) || 'Failed to update rewrite.'});
            }
            await fetchStatus(teamID);
            setEditingRewrite(null);
            setRewriteInput('');
            statusTimerRef.current = setTimeout(() => setStatus({loading: false}), STATUS_DISPLAY_MS);
        } catch {
            setStatus({loading: false, success: false, message: 'Network error.'});
            statusTimerRef.current = setTimeout(() => setStatus({loading: false}), STATUS_DISPLAY_MS);
        }
    }, [callAPI, teamID, fetchStatus]);

    const handleClearRewrite = React.useCallback(async (connName: string) => {
        if (!teamID) {
            return;
        }
        setStatus({loading: true});
        if (statusTimerRef.current) {
            clearTimeout(statusTimerRef.current);
        }
        try {
            const url = `/plugins/${manifest.id}/api/v1/teams/${teamID}/rewrite?connection=${encodeURIComponent(connName)}`;
            const response = await fetch(url, {
                method: 'DELETE',
                credentials: 'same-origin',
                headers: {
                    'Content-Type': 'application/json',
                    'X-CSRF-Token': getCSRFToken(),
                    'X-Requested-With': 'XMLHttpRequest',
                },
            });
            const data = await response.json();
            if (response.ok) {
                setStatus({loading: false, success: true, message: `Remote team rewrite cleared for "${connName}".`});
            } else {
                setStatus({loading: false, success: false, message: (data.error as string) || 'Failed to clear rewrite.'});
            }
            await fetchStatus(teamID);
            setEditingRewrite(null);
            setRewriteInput('');
            statusTimerRef.current = setTimeout(() => setStatus({loading: false}), STATUS_DISPLAY_MS);
        } catch {
            setStatus({loading: false, success: false, message: 'Network error.'});
            statusTimerRef.current = setTimeout(() => setStatus({loading: false}), STATUS_DISPLAY_MS);
        }
    }, [teamID, fetchStatus]);

    const handleBackdropClick = React.useCallback((e: React.MouseEvent) => {
        if (e.target === e.currentTarget) {
            setTeamID(null);
        }
    }, []);

    if (!teamID) {
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
        rewriteRow: {
            display: 'flex',
            alignItems: 'center',
            gap: '6px',
            marginTop: '4px',
            fontSize: '12px',
            color: colors.textMuted,
        },
        rewriteLabel: {
            fontSize: '12px',
            color: colors.textMuted,
        },
        rewriteValue: {
            fontSize: '12px',
            fontWeight: 600,
            color: colors.textMuted,
        },
        rewriteBtn: {
            background: 'none',
            border: 'none',
            cursor: 'pointer',
            fontSize: '11px',
            fontWeight: 600,
            color: colors.primary,
            padding: '0 4px',
        },
        rewriteEditRow: {
            display: 'flex',
            alignItems: 'center',
            gap: '6px',
            marginTop: '6px',
        },
        rewriteInput: {
            fontSize: '12px',
            padding: '4px 8px',
            border: `1px solid ${colors.border}`,
            borderRadius: '4px',
            outline: 'none',
            color: colors.text,
            background: colors.bg,
            flex: 1,
            maxWidth: '200px',
        },
        rewriteSaveBtn: {
            background: colors.primary,
            border: 'none',
            cursor: 'pointer',
            fontSize: '11px',
            fontWeight: 600,
            color: '#fff',
            padding: '4px 10px',
            borderRadius: '4px',
        },
        rewriteCancelBtn: {
            background: 'none',
            border: `1px solid ${colors.border}`,
            cursor: 'pointer',
            fontSize: '11px',
            fontWeight: 600,
            color: colors.textMuted,
            padding: '3px 10px',
            borderRadius: '4px',
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

    const renderConnectionButton = (conn: ConnectionStatus, isActioning: boolean) => {
        if (conn.linked) {
            return (
                <button
                    style={s.btnUnlink}
                    onClick={() => handleToggle(conn, true)}
                    disabled={actionInProgress !== null}
                >
                    {isActioning ? 'Unlinking...' : 'Unlink'}
                </button>
            );
        }
        if (conn.request_pending) {
            return (
                <button
                    style={{...s.btnLink, opacity: 0.5, cursor: 'default'}}
                    disabled={true}
                    title={'A connection request is awaiting system admin approval'}
                >
                    {'Request Pending'}
                </button>
            );
        }
        let linkLabel = 'Link';
        let linkingLabel = 'Linking...';
        if (requestMode) {
            linkLabel = 'Request Link';
            linkingLabel = 'Requesting...';
        }
        return (
            <button
                style={s.btnLink}
                onClick={() => handleToggle(conn, false)}
                disabled={actionInProgress !== null}
            >
                {isActioning ? linkingLabel : linkLabel}
            </button>
        );
    };

    const renderBody = () => {
        if (fetching) {
            return <p style={{...s.emptyState, color: colors.textMuted}}>{'Loading...'}</p>;
        }

        if (connections.length === 0) {
            return (
                <p style={s.emptyState}>
                    {'No connections available. Configure connections in the System Console.'}
                </p>
            );
        }

        return (
            <div>
                {connections.map((conn) => {
                    const direction = conn.direction;
                    const isInbound = direction === 'inbound';
                    const accentColor = isInbound ? colors.inbound : colors.outbound;
                    const connKey = `${conn.direction}:${conn.name}`;
                    const isActioning = actionInProgress === connKey;

                    const badgeStyle: React.CSSProperties = {
                        ...s.directionBadge,
                        background: accentColor + '1A',
                        color: accentColor,
                    };

                    const provLabel = providerLabel(conn.provider);
                    const badgeLabel = isInbound ? `${provLabel} \u2192 MATTERMOST` : `MATTERMOST \u2192 ${provLabel}`;

                    const isEditingThis = editingRewrite === connKey;

                    return (
                        <div
                            key={connKey}
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
                                {isInbound && !isEditingThis && (
                                    <div style={s.rewriteRow}>
                                        {conn.remote_team_name ? (
                                            <>
                                                <span style={s.rewriteLabel}>{'Remote team:'}</span>
                                                <span style={s.rewriteValue}>{conn.remote_team_name}</span>
                                                <button
                                                    style={s.rewriteBtn}
                                                    onClick={() => {
                                                        setEditingRewrite(connKey);
                                                        setRewriteInput(conn.remote_team_name || '');
                                                    }}
                                                >
                                                    {'Edit'}
                                                </button>
                                                <button
                                                    style={{...s.rewriteBtn, color: colors.danger}}
                                                    onClick={() => handleClearRewrite(conn.name)}
                                                >
                                                    {'Clear'}
                                                </button>
                                            </>
                                        ) : (
                                            <>
                                                <span style={s.rewriteLabel}>{'No remote team rewrite'}</span>
                                                <button
                                                    style={s.rewriteBtn}
                                                    onClick={() => {
                                                        setEditingRewrite(connKey);
                                                        setRewriteInput('');
                                                    }}
                                                >
                                                    {'Set'}
                                                </button>
                                            </>
                                        )}
                                    </div>
                                )}
                                {isInbound && isEditingThis && (
                                    <div style={s.rewriteEditRow}>
                                        <input
                                            style={s.rewriteInput}
                                            type={'text'}
                                            placeholder={'Remote team name'}
                                            value={rewriteInput}
                                            onChange={(e) => setRewriteInput(e.target.value)}
                                            onKeyDown={(e) => {
                                                if (e.key === 'Enter' && rewriteInput.trim()) {
                                                    handleSaveRewrite(conn.name, rewriteInput.trim());
                                                }
                                                if (e.key === 'Escape') {
                                                    e.stopPropagation();
                                                    setEditingRewrite(null);
                                                    setRewriteInput('');
                                                }
                                            }}
                                            autoFocus={true}
                                        />
                                        <button
                                            style={s.rewriteSaveBtn}
                                            disabled={!rewriteInput.trim()}
                                            onClick={() => handleSaveRewrite(conn.name, rewriteInput.trim())}
                                        >
                                            {'Save'}
                                        </button>
                                        <button
                                            style={s.rewriteCancelBtn}
                                            onClick={() => {
                                                setEditingRewrite(null);
                                                setRewriteInput('');
                                            }}
                                        >
                                            {'Cancel'}
                                        </button>
                                    </div>
                                )}
                                <div style={s.rewriteRow}>
                                    {conn.file_transfer_enabled ? (
                                        <span style={s.rewriteLabel}>
                                            {'\u{1F4CE} Files: '}
                                            {conn.file_filter_mode === 'allow' && `Allow ${conn.file_filter_types}`}
                                            {conn.file_filter_mode === 'deny' && `Deny ${conn.file_filter_types}`}
                                            {!conn.file_filter_mode && 'All types'}
                                        </span>
                                    ) : (
                                        <span style={s.rewriteLabel}>{'\u{1F6AB} Files: Disabled'}</span>
                                    )}
                                </div>
                            </div>
                            {renderConnectionButton(conn, isActioning)}
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
                    <h2 style={s.title}>{`Cross Guard Settings for ${teamName || '...'}`}</h2>
                    <button
                        style={s.closeBtn}
                        onClick={() => setTeamID(null)}
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

export default CrossguardTeamModal;
