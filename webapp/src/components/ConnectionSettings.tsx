import manifest from 'manifest';
import React from 'react';

type ProviderType = 'nats' | 'azure-queue' | 'azure-blob' | 'azure-servicebus';

// Azure auth modes per the server's per-provider auth_mode discriminator
// (see server/configuration.go). Empty string is intentionally allowed:
// the server treats empty as the legacy mode for each provider (shared-key
// for Queue/Blob, connection-string for Service Bus). The webapp always
// writes one of the explicit constants when a form is saved.
type AzureQueueAuthMode = '' | 'shared-key' | 'service-principal';
type AzureBlobAuthMode = '' | 'shared-key' | 'service-principal';
type AzureServiceBusAuthMode = '' | 'connection-string' | 'service-principal';
type AzureCloud = '' | 'public' | 'usgov' | 'china';

const AZURE_AUTH_SHARED_KEY = 'shared-key';
const AZURE_AUTH_CONNECTION_STRING = 'connection-string';
const AZURE_AUTH_SERVICE_PRINCIPAL = 'service-principal';
const AZURE_CLOUD_PUBLIC = 'public';
const AZURE_CLOUD_USGOV = 'usgov';
const AZURE_CLOUD_CHINA = 'china';

// SECRET_SENTINEL is sent back by the form for any password field the
// admin did not touch. The server's OnConfigurationChange merge step
// (server/configuration.go mergeOneConnectionSecrets) sees this string
// and substitutes the stored value, so unchanged secrets never round-trip
// through the browser as cleartext on save. The token is deliberately
// distinctive (not a row of asterisks) so it cannot collide with a real
// secret. Keep in sync with the Go-side SecretSentinel constant.
const SECRET_SENTINEL = '__CROSSGUARD_SECRET_UNCHANGED__';

interface NATSProviderConfig {
    address: string;
    subject: string;
    tls_enabled: boolean;
    auth_type: 'none' | 'token' | 'credentials';
    token: string;
    username: string;
    password: string;
    client_cert: string;
    client_key: string;
    ca_cert: string;
}

// Shared Azure Service Principal fields. Mirrors the Go-side flat fields
// added to each provider config. client_secret is required in SP mode.
// To keep the secret out of plugin config, inject the whole connections
// JSON via the MM_PLUGINSETTINGS_PLUGINS_CROSSGUARD_* env var.
interface AzureSPFields {
    auth_mode?: string; // typed as string here for TS variance across providers
    azure_cloud?: AzureCloud;
    tenant_id?: string;
    client_id?: string;
    client_secret?: string;
}

interface AzureQueueProviderConfig extends AzureSPFields {
    queue_service_url: string;
    blob_service_url: string;
    account_name: string;
    account_key: string;
    queue_name: string;
    blob_container_name: string;
    auth_mode?: AzureQueueAuthMode;
}

interface AzureBlobProviderConfig extends AzureSPFields {
    service_url: string;
    account_name: string;
    account_key: string;
    blob_container_name: string;
    flush_interval_seconds?: number;
    auth_mode?: AzureBlobAuthMode;
}

interface AzureServiceBusProviderConfig extends AzureSPFields {
    connection_string: string;
    queue_name: string;
    blob_service_url: string;
    blob_account_name: string;
    blob_account_key: string;
    blob_container_name: string;
    max_message_size_bytes?: number;
    blob_poll_interval_seconds?: number;
    auth_mode?: AzureServiceBusAuthMode;
    service_bus_namespace?: string;
}

interface Connection {
    name: string;
    provider: ProviderType;
    file_transfer_enabled: boolean;
    file_filter_mode: '' | 'allow' | 'deny';
    file_filter_types: string;
    message_format: 'json' | 'xml';
    nats?: NATSProviderConfig;
    azure_queue?: AzureQueueProviderConfig;
    azure_blob?: AzureBlobProviderConfig;
    azure_servicebus?: AzureServiceBusProviderConfig;
}

interface CustomSettingProps {
    id: string;
    value: string;
    onChange: (id: string, value: string) => void;
    setSaveNeeded: () => void;
    disabled: boolean;
    config: object;
    currentState: object;
    license: object;
    setByEnv: boolean;
}

interface TestStatus {
    loading: boolean;
    success?: boolean;
    message?: string;
}

const DEFAULT_NATS_ADDRESS = 'nats://localhost:4222';
const TEST_STATUS_DISPLAY_MS = 10000;
const SUBJECT_PREFIX = 'crossguard.';

function getCSRFToken(): string {
    const match = document.cookie.match(/MMCSRF=([^;]+)/);
    return match ? match[1] : '';
}

const emptyNATSConfig: NATSProviderConfig = {
    address: DEFAULT_NATS_ADDRESS,
    subject: '',
    tls_enabled: false,
    auth_type: 'none',
    token: '',
    username: '',
    password: '',
    client_cert: '',
    client_key: '',
    ca_cert: '',
};

const emptyAzureQueueConfig: AzureQueueProviderConfig = {
    queue_service_url: '',
    blob_service_url: '',
    account_name: '',
    account_key: '',
    queue_name: '',
    blob_container_name: '',
    auth_mode: AZURE_AUTH_SHARED_KEY,
    azure_cloud: AZURE_CLOUD_PUBLIC,
    tenant_id: '',
    client_id: '',
    client_secret: '',
};

const emptyAzureBlobConfig: AzureBlobProviderConfig = {
    service_url: '',
    account_name: '',
    account_key: '',
    blob_container_name: '',
    flush_interval_seconds: 60,
    auth_mode: AZURE_AUTH_SHARED_KEY,
    azure_cloud: AZURE_CLOUD_PUBLIC,
    tenant_id: '',
    client_id: '',
    client_secret: '',
};

const emptyAzureServiceBusConfig: AzureServiceBusProviderConfig = {
    connection_string: '',
    queue_name: '',
    blob_service_url: '',
    blob_account_name: '',
    blob_account_key: '',
    blob_container_name: '',
    auth_mode: AZURE_AUTH_CONNECTION_STRING,
    azure_cloud: AZURE_CLOUD_PUBLIC,
    service_bus_namespace: '',
    tenant_id: '',
    client_id: '',
    client_secret: '',
};

const emptyConnection: Connection = {
    name: '',
    provider: 'nats',
    file_transfer_enabled: false,
    file_filter_mode: '',
    file_filter_types: '',
    message_format: 'json',
    nats: {...emptyNATSConfig},
};

// loadConnectionForEdit prepares a stored connection for the edit form.
// Every secret field that already has a value is replaced with the
// SECRET_SENTINEL so the form never displays cleartext secrets and so the
// server's merge step can preserve the stored value when the admin saves
// without editing the field. Secret fields touched as: account_key
// (Queue/Blob), client_secret (Queue/Blob/SB), connection_string (SB),
// blob_account_key (SB).
// validateAzureAuthFields runs client-side auth-mode validation for the
// three Azure providers. legacyMode is the legacy default for the
// provider ("shared-key" for Queue/Blob; "connection-string" for SB).
// Empty cfg.auth_mode means "use legacy" and gets the legacy required
// fields enforced. The server runs the canonical version of these
// checks; the client copy is for fast inline feedback.
function validateAzureAuthFields(
    cfg: AzureSPFields & {
        account_key?: string;
        account_name?: string;
        connection_string?: string;
        service_bus_namespace?: string;
    },
    legacyMode: 'shared-key' | 'connection-string',
): string | null {
    const mode = (cfg.auth_mode || legacyMode) as string;

    if (mode === legacyMode) {
        if (legacyMode === 'shared-key') {
            if (!cfg.account_name || !cfg.account_name.trim()) {
                return 'Account Name is required for shared-key auth mode.';
            }
            if (!cfg.account_key || !cfg.account_key.trim()) {
                return 'Account Key is required for shared-key auth mode.';
            }
        } else if (!cfg.connection_string || !cfg.connection_string.trim()) {
                return 'Connection String is required for connection-string auth mode.';
            }

        // Cross-mode leakage: SP fields must not be set in legacy mode.
        if (cfg.tenant_id || cfg.client_id || cfg.client_secret) {
            return `Service Principal fields (tenant_id / client_id / client_secret) must be empty when auth_mode is ${legacyMode}.`;
        }
        return null;
    }

    if (mode === AZURE_AUTH_SERVICE_PRINCIPAL) {
        if (!cfg.tenant_id || !cfg.tenant_id.trim()) {
            return 'Tenant ID is required for service-principal auth mode.';
        }
        if (!isValidAzureTenantID(cfg.tenant_id)) {
            return 'Tenant ID must be a GUID or FQDN (e.g. contoso.onmicrosoft.com).';
        }
        if (!cfg.client_id || !cfg.client_id.trim()) {
            return 'Client ID is required for service-principal auth mode.';
        }
        if (!cfg.client_secret || !cfg.client_secret.trim()) {
            return 'Client Secret is required for service-principal auth mode.';
        }

        // Cross-mode leakage: legacy secrets must be empty in SP mode.
        // Note: SECRET_SENTINEL is allowed in account_key etc. because the
        // server resolves it back to the stored value before validating.
        const isSentinelOrEmpty = (v?: string) => !v || v === SECRET_SENTINEL;
        if (legacyMode === 'shared-key' && !isSentinelOrEmpty(cfg.account_key)) {
            return 'Account Key must be empty when auth_mode is service-principal.';
        }
        if (legacyMode === 'connection-string' && !isSentinelOrEmpty(cfg.connection_string)) {
            return 'Connection String must be empty when auth_mode is service-principal.';
        }
        if (legacyMode === 'connection-string') {
            if (!cfg.service_bus_namespace || !cfg.service_bus_namespace.trim()) {
                return 'Service Bus Namespace is required for service-principal auth mode (e.g. myns.servicebus.windows.net).';
            }
        }
        return null;
    }

    return `Invalid auth_mode "${mode}".`;
}

// isValidAzureTenantID accepts either a GUID or an FQDN-shaped string.
// Mirrors the server-side validateAzureTenantID in configuration.go.
function isValidAzureTenantID(s: string): boolean {
    const t = s.trim();
    if (!t) {
        return false;
    }
    const guid = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/;
    if (guid.test(t)) {
        return true;
    }

    // FQDN-shape: must contain a dot, no scheme, no path, no whitespace.
    if (!t.includes('.') || (/[\s/\\]/).test(t) || t.includes('://')) {
        return false;
    }
    return true;
}

// ServicePrincipalFields renders the shared SP credential inputs
// (tenant_id, client_id, client_secret). Reused across all three Azure
// provider forms so the UI is consistent. Styles are read from the
// lexically-enclosed `styles` object; this component is intentionally
// declared inside the same module to avoid duplicating the styles object.
function ServicePrincipalFieldsComponent({
    ariaPrefix,
    cfg,
    onChange,
    disabled,
}: {
    ariaPrefix: string;
    cfg: AzureSPFields;
    onChange: (field: string, value: string) => void;
    disabled: boolean;
}) {
    return (
        <>
            <div style={styles.formSection}>
                <div style={styles.formSectionTitle}>{'Service Principal'}</div>
                <div style={styles.helpText}>
                    {'Azure AD application credentials. The application must have data-plane RBAC (e.g. "Storage Queue Data Contributor" or "Storage Blob Data Contributor" or "Azure Service Bus Data Sender/Receiver") on the target resource. The plugin does NOT auto-create queues/containers in service-principal mode.'}
                </div>
                <div style={styles.inputGroup}>
                    <label style={styles.label}>{'Tenant ID'}</label>
                    <input
                        aria-label={`${ariaPrefix} Tenant ID`}
                        style={styles.input}
                        type='text'
                        value={cfg.tenant_id || ''}
                        onChange={(e) => onChange('tenant_id', e.target.value)}
                        disabled={disabled}
                        placeholder='00000000-0000-0000-0000-000000000000 or contoso.onmicrosoft.com'
                    />
                </div>
                <div style={styles.inputGroup}>
                    <label style={styles.label}>{'Client ID'}</label>
                    <input
                        aria-label={`${ariaPrefix} Client ID`}
                        style={styles.input}
                        type='text'
                        value={cfg.client_id || ''}
                        onChange={(e) => onChange('client_id', e.target.value)}
                        disabled={disabled}
                        placeholder='Application (client) ID'
                    />
                </div>
                <div style={styles.inputGroup}>
                    <label style={styles.label}>{'Client Secret'}</label>
                    <input
                        aria-label={`${ariaPrefix} Client Secret`}
                        style={styles.input}
                        type='password'
                        value={cfg.client_secret || ''}
                        onChange={(e) => onChange('client_secret', e.target.value)}
                        disabled={disabled}
                        placeholder='Azure AD application client secret'
                    />
                    <div style={styles.helpText}>
                        {'To keep the secret out of plugin config on disk, inject the whole connections JSON via the MM_PLUGINSETTINGS_PLUGINS_CROSSGUARD_OUTBOUNDCONNECTIONS / INBOUNDCONNECTIONS env var (Kubernetes Secret, CSI volume + init container, systemd LoadCredential=, or Vault).'}
                    </div>
                </div>
            </div>
        </>
    );
}

function loadConnectionForEdit(stored: Connection): Connection {
    const next: Connection = {...stored};
    if (next.azure_queue) {
        next.azure_queue = {
            ...next.azure_queue,
            account_key: next.azure_queue.account_key ? SECRET_SENTINEL : '',
            client_secret: next.azure_queue.client_secret ? SECRET_SENTINEL : '',
        };
    }
    if (next.azure_blob) {
        next.azure_blob = {
            ...next.azure_blob,
            account_key: next.azure_blob.account_key ? SECRET_SENTINEL : '',
            client_secret: next.azure_blob.client_secret ? SECRET_SENTINEL : '',
        };
    }
    if (next.azure_servicebus) {
        next.azure_servicebus = {
            ...next.azure_servicebus,
            connection_string: next.azure_servicebus.connection_string ? SECRET_SENTINEL : '',
            blob_account_key: next.azure_servicebus.blob_account_key ? SECRET_SENTINEL : '',
            client_secret: next.azure_servicebus.client_secret ? SECRET_SENTINEL : '',
        };
    }
    return next;
}

function normalizeConnection(conn: Record<string, unknown>): Connection {
    if (conn.provider) {
        return conn as unknown as Connection;
    }
    return {
        name: (conn.name as string) || '',
        provider: 'nats',
        file_transfer_enabled: Boolean(conn.file_transfer_enabled),
        file_filter_mode: (conn.file_filter_mode as '' | 'allow' | 'deny') || '',
        file_filter_types: (conn.file_filter_types as string) || '',
        message_format: (conn.message_format as 'json' | 'xml') || 'json',
        nats: {
            address: (conn.address as string) || DEFAULT_NATS_ADDRESS,
            subject: (conn.subject as string) || '',
            tls_enabled: Boolean(conn.tls_enabled),
            auth_type: (conn.auth_type as 'none' | 'token' | 'credentials') || 'none',
            token: (conn.token as string) || '',
            username: (conn.username as string) || '',
            password: (conn.password as string) || '',
            client_cert: (conn.client_cert as string) || '',
            client_key: (conn.client_key as string) || '',
            ca_cert: (conn.ca_cert as string) || '',
        },
    };
}

const colors = {
    primary: '#1C58D9',
    primaryHover: '#1851C4',
    danger: '#D24B4E',
    success: '#3DB887',
    border: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.16)',
    borderStrong: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.24)',
    bg: 'var(--center-channel-bg, #fff)',
    bgAlt: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.04)',
    text: 'var(--center-channel-color, #3D3C40)',
    textMuted: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.64)',
    textSubtle: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.48)',
};

const styles = {
    container: {
        padding: '0 0 8px',
    } as React.CSSProperties,
    card: {
        border: `1px solid ${colors.border}`,
        borderRadius: '8px',
        padding: '16px 20px',
        marginBottom: '12px',
        background: colors.bg,
        transition: 'box-shadow 0.15s ease',
    } as React.CSSProperties,
    cardHeader: {
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        marginBottom: '12px',
    } as React.CSSProperties,
    cardTitle: {
        display: 'flex',
        alignItems: 'center',
        gap: '8px',
        fontSize: '15px',
        fontWeight: 600,
        color: colors.text,
        margin: 0,
    } as React.CSSProperties,
    cardMeta: {
        display: 'grid',
        gridTemplateColumns: '1fr 1fr',
        gap: '4px 24px',
        marginBottom: '14px',
    } as React.CSSProperties,
    cardMetaItem: {
        fontSize: '13px',
        color: colors.textMuted,
        lineHeight: '20px',
    } as React.CSSProperties,
    cardMetaLabel: {
        fontWeight: 600,
        color: colors.textSubtle,
        marginRight: '4px',
    } as React.CSSProperties,
    cardActions: {
        display: 'flex',
        alignItems: 'center',
        gap: '8px',
        paddingTop: '12px',
        borderTop: `1px solid ${colors.border}`,
    } as React.CSSProperties,
    form: {
        border: `1px solid ${colors.primary}`,
        borderRadius: '8px',
        padding: '24px',
        marginBottom: '12px',
        background: colors.bg,
    } as React.CSSProperties,
    formTitle: {
        fontSize: '15px',
        fontWeight: 600,
        color: colors.text,
        margin: '0 0 20px',
        paddingBottom: '12px',
        borderBottom: `1px solid ${colors.border}`,
    } as React.CSSProperties,
    formSection: {
        marginBottom: '20px',
    } as React.CSSProperties,
    formSectionTitle: {
        fontSize: '12px',
        fontWeight: 600,
        textTransform: 'uppercase',
        letterSpacing: '0.5px',
        color: colors.textSubtle,
        marginBottom: '12px',
    } as React.CSSProperties,
    formRow: {
        display: 'grid',
        gridTemplateColumns: '1fr 1fr',
        gap: '16px',
    } as React.CSSProperties,
    inputGroup: {
        marginBottom: '16px',
    } as React.CSSProperties,
    label: {
        display: 'block',
        marginBottom: '6px',
        fontWeight: 600,
        fontSize: '13px',
        color: colors.text,
    } as React.CSSProperties,
    input: {
        width: '100%',
        padding: '8px 12px',
        border: `1px solid ${colors.border}`,
        borderRadius: '4px',
        fontSize: '14px',
        lineHeight: '20px',
        boxSizing: 'border-box',
        color: colors.text,
        background: colors.bg,
        outline: 'none',
    } as React.CSSProperties,
    inputDisabled: {
        width: '100%',
        padding: '8px 12px',
        border: `1px solid ${colors.border}`,
        borderRadius: '4px',
        fontSize: '14px',
        lineHeight: '20px',
        boxSizing: 'border-box',
        color: colors.textMuted,
        background: colors.bgAlt,
        outline: 'none',
    } as React.CSSProperties,
    select: {
        padding: '8px 12px',
        border: `1px solid ${colors.border}`,
        borderRadius: '4px',
        fontSize: '14px',
        lineHeight: '20px',
        color: colors.text,
        background: colors.bg,
        outline: 'none',
    } as React.CSSProperties,
    helpText: {
        fontSize: '12px',
        color: colors.textSubtle,
        marginTop: '4px',
        lineHeight: '16px',
    } as React.CSSProperties,
    checkbox: {
        display: 'flex',
        alignItems: 'center',
        gap: '8px',
        fontSize: '14px',
        fontWeight: 500,
        color: colors.text,
        cursor: 'pointer',
    } as React.CSSProperties,
    badge: {
        display: 'inline-flex',
        alignItems: 'center',
        padding: '2px 8px',
        borderRadius: '10px',
        fontSize: '11px',
        fontWeight: 600,
        lineHeight: '16px',
        textTransform: 'uppercase',
        letterSpacing: '0.3px',
    } as React.CSSProperties,
    badgeAuth: {
        background: 'rgba(var(--center-channel-color-rgb, 61, 60, 64), 0.08)',
        color: colors.textMuted,
    } as React.CSSProperties,
    badgeTls: {
        background: 'rgba(56, 111, 229, 0.12)',
        color: colors.primary,
    } as React.CSSProperties,
    btnPrimary: {
        display: 'inline-flex',
        alignItems: 'center',
        gap: '6px',
        padding: '8px 16px',
        cursor: 'pointer',
        border: 'none',
        borderRadius: '4px',
        background: colors.primary,
        color: '#fff',
        fontSize: '13px',
        fontWeight: 600,
        lineHeight: '18px',
    } as React.CSSProperties,
    btnSecondary: {
        display: 'inline-flex',
        alignItems: 'center',
        gap: '6px',
        padding: '8px 16px',
        cursor: 'pointer',
        border: `1px solid ${colors.border}`,
        borderRadius: '4px',
        background: colors.bg,
        color: colors.text,
        fontSize: '13px',
        fontWeight: 600,
        lineHeight: '18px',
    } as React.CSSProperties,
    btnDanger: {
        display: 'inline-flex',
        alignItems: 'center',
        gap: '6px',
        padding: '8px 16px',
        cursor: 'pointer',
        border: `1px solid ${colors.danger}`,
        borderRadius: '4px',
        background: 'transparent',
        color: colors.danger,
        fontSize: '13px',
        fontWeight: 600,
        lineHeight: '18px',
    } as React.CSSProperties,
    btnSmall: {
        padding: '6px 12px',
        fontSize: '12px',
    } as React.CSSProperties,
    formActions: {
        display: 'flex',
        alignItems: 'center',
        gap: '8px',
        paddingTop: '16px',
        borderTop: `1px solid ${colors.border}`,
    } as React.CSSProperties,
    statusBanner: {
        display: 'flex',
        alignItems: 'center',
        gap: '8px',
        padding: '8px 12px',
        borderRadius: '4px',
        fontSize: '13px',
        fontWeight: 500,
        marginTop: '12px',
    } as React.CSSProperties,
    statusSuccess: {
        background: 'rgba(61, 184, 135, 0.12)',
        color: '#1B8A5C',
    } as React.CSSProperties,
    statusError: {
        background: 'rgba(210, 75, 78, 0.12)',
        color: colors.danger,
    } as React.CSSProperties,
    formError: {
        display: 'flex',
        alignItems: 'center',
        gap: '6px',
        padding: '8px 12px',
        borderRadius: '4px',
        background: 'rgba(210, 75, 78, 0.08)',
        color: colors.danger,
        fontSize: '13px',
        marginBottom: '16px',
    } as React.CSSProperties,
    emptyState: {
        textAlign: 'center',
        padding: '32px 20px',
        color: colors.textSubtle,
        fontSize: '14px',
        border: `1px dashed ${colors.border}`,
        borderRadius: '8px',
    } as React.CSSProperties,
    addArea: {
        marginBottom: '16px',
    } as React.CSSProperties,
    sectionHeader: {
        marginBottom: '16px',
    } as React.CSSProperties,
    sectionTitle: {
        display: 'flex',
        alignItems: 'center',
        gap: '10px',
        fontSize: '16px',
        fontWeight: 600,
        color: colors.text,
        margin: '0 0 4px',
    } as React.CSSProperties,
    sectionDesc: {
        fontSize: '13px',
        color: colors.textSubtle,
        margin: 0,
        lineHeight: '20px',
    } as React.CSSProperties,
    directionBadge: {
        display: 'inline-flex',
        alignItems: 'center',
        padding: '2px 10px',
        borderRadius: '10px',
        fontSize: '11px',
        fontWeight: 600,
        lineHeight: '16px',
        textTransform: 'uppercase',
        letterSpacing: '0.3px',
    } as React.CSSProperties,
    directionInbound: {
        background: 'rgba(61, 184, 135, 0.12)',
        color: '#1B8A5C',
    } as React.CSSProperties,
    directionOutbound: {
        background: 'rgba(56, 111, 229, 0.12)',
        color: colors.primary,
    } as React.CSSProperties,
};

function authLabel(authType: string): string {
    switch (authType) {
    case 'token':
        return 'Token';
    case 'credentials':
        return 'Credentials';
    default:
        return 'None';
    }
}

const ConnectionSettings: React.FC<CustomSettingProps> = ({
    id,
    value,
    onChange,
    setSaveNeeded,
    disabled,
}) => {
    const [connections, setConnections] = React.useState<Connection[]>([]);
    const [editingIndex, setEditingIndex] = React.useState<number | null>(null);
    const [editForm, setEditForm] = React.useState<Connection>({...emptyConnection});
    const [testStatus, setTestStatus] = React.useState<Record<number, TestStatus>>({});
    const [formError, setFormError] = React.useState<string | null>(null);

    React.useEffect(() => {
        try {
            const parsed = value ? JSON.parse(value) : [];
            if (Array.isArray(parsed)) {
                setConnections(parsed.map((c: Record<string, unknown>) => normalizeConnection(c)));
            } else {
                setConnections([]);
            }
        } catch {
            setConnections([]);
        }
    }, [value]);

    const isInbound = id.toLowerCase().includes('inbound');

    const handleAdd = () => {
        setEditingIndex(-1);
        setEditForm({...emptyConnection});
        setFormError(null);
    };

    const handleEdit = (index: number) => {
        setEditingIndex(index);
        setEditForm(loadConnectionForEdit(connections[index]));
        setFormError(null);
    };

    const handleDelete = (index: number) => {
        const updated: Connection[] = connections.filter((_, i) => i !== index);
        const json = JSON.stringify(updated);
        onChange(id, json);
        setSaveNeeded();
        if (editingIndex === index) {
            setEditingIndex(null);
        } else if (editingIndex !== null && editingIndex > index) {
            setEditingIndex(editingIndex - 1);
        }
        setTestStatus((prev) => {
            const next: Record<number, TestStatus> = {};
            for (const [key, val] of Object.entries(prev)) {
                const k = Number(key);
                if (k < index) {
                    next[k] = val;
                } else if (k > index) {
                    next[k - 1] = val;
                }
            }
            return next;
        });
    };

    const handleSave = () => {
        const trimmedName = editForm.name.trim();
        if (!trimmedName) {
            setFormError('Name is required.');
            return;
        }

        const isDuplicate = connections.some(
            (conn, i) => i !== editingIndex && conn.name.trim() === trimmedName,
        );
        if (isDuplicate) {
            setFormError('A connection with this name already exists. Please use a unique name.');
            return;
        }

        setFormError(null);

        let cleanedForm: Connection;

        if (editForm.provider === 'nats') {
            const nats = editForm.nats || emptyNATSConfig;
            if (!nats.address.trim()) {
                setFormError('Address is required.');
                return;
            }

            if (nats.auth_type === 'token' && !nats.token.trim()) {
                setFormError('Token is required when auth type is Token.');
                return;
            }

            if (nats.auth_type === 'credentials' && (!nats.username.trim() || !nats.password.trim())) {
                setFormError('Username and password are required when auth type is Credentials.');
                return;
            }

            const trimmedSubject = nats.subject.trim();
            if (!trimmedSubject) {
                setFormError('Subject is required.');
                return;
            }
            if (!trimmedSubject.startsWith(SUBJECT_PREFIX)) {
                setFormError(`Subject must start with "${SUBJECT_PREFIX}".`);
                return;
            }

            cleanedForm = {
                ...editForm,
                name: trimmedName,
                nats: {...nats, subject: trimmedSubject},
                azure_queue: undefined,
                azure_blob: undefined,
            };
        } else if (editForm.provider === 'azure-queue') {
            const azureQueue = editForm.azure_queue || emptyAzureQueueConfig;
            if (!azureQueue.queue_service_url.trim()) {
                setFormError('Queue Service URL is required.');
                return;
            }
            if (editForm.file_transfer_enabled && !azureQueue.blob_service_url.trim()) {
                setFormError('Blob Service URL is required when file transfer is enabled.');
                return;
            }
            if (!azureQueue.queue_name.trim()) {
                setFormError('Queue Name is required.');
                return;
            }
            if (editForm.file_transfer_enabled && !azureQueue.blob_container_name.trim()) {
                setFormError('Blob Container Name is required when file transfer is enabled.');
                return;
            }

            const queueAuthErr = validateAzureAuthFields(azureQueue, 'shared-key');
            if (queueAuthErr) {
                setFormError(queueAuthErr);
                return;
            }

            cleanedForm = {
                ...editForm,
                name: trimmedName,
                azure_queue: {...azureQueue},
                nats: undefined,
                azure_blob: undefined,
                azure_servicebus: undefined,
            };
        } else if (editForm.provider === 'azure-servicebus') {
            const sb = editForm.azure_servicebus || emptyAzureServiceBusConfig;
            if (!sb.queue_name.trim()) {
                setFormError('Queue Name is required.');
                return;
            }
            const sbAuthErr = validateAzureAuthFields(sb, 'connection-string');
            if (sbAuthErr) {
                setFormError(sbAuthErr);
                return;
            }
            if (editForm.file_transfer_enabled) {
                if (!sb.blob_service_url.trim()) {
                    setFormError('Blob Service URL is required when file transfer is enabled.');
                    return;
                }
                if (!sb.blob_container_name.trim()) {
                    setFormError('Blob Container Name is required when file transfer is enabled.');
                    return;
                }

                // Blob sidecar credential model is auth_mode dependent:
                // connection-string mode requires explicit blob_account_*;
                // service-principal mode inherits parent SP and forbids them.
                const authMode = sb.auth_mode || AZURE_AUTH_CONNECTION_STRING;
                if (authMode === AZURE_AUTH_CONNECTION_STRING) {
                    if (!sb.blob_account_name.trim()) {
                        setFormError('Blob Account Name is required when file transfer is enabled.');
                        return;
                    }
                    if (!sb.blob_account_key.trim()) {
                        setFormError('Blob Account Key is required when file transfer is enabled.');
                        return;
                    }
                }
            }

            cleanedForm = {
                ...editForm,
                name: trimmedName,
                azure_servicebus: {...sb},
                nats: undefined,
                azure_queue: undefined,
                azure_blob: undefined,
            };
        } else {
            const azureBlob = editForm.azure_blob || emptyAzureBlobConfig;
            if (!azureBlob.service_url.trim()) {
                setFormError('Service URL is required.');
                return;
            }
            if (!azureBlob.blob_container_name.trim()) {
                setFormError('Blob Container Name is required.');
                return;
            }
            const flush = azureBlob.flush_interval_seconds;
            if (flush === undefined || Number.isNaN(flush) || flush < 5) {
                setFormError('Flush Interval must be at least 5 seconds.');
                return;
            }
            const blobAuthErr = validateAzureAuthFields(azureBlob, 'shared-key');
            if (blobAuthErr) {
                setFormError(blobAuthErr);
                return;
            }

            cleanedForm = {
                ...editForm,
                name: trimmedName,
                azure_blob: {...azureBlob},
                nats: undefined,
                azure_queue: undefined,
                azure_servicebus: undefined,
            };
        }

        let updated: Connection[];
        if (editingIndex === -1) {
            updated = [...connections, cleanedForm];
        } else if (editingIndex === null) {
            return;
        } else {
            updated = connections.map((conn, i) => (i === editingIndex ? cleanedForm : conn));
        }

        const json = JSON.stringify(updated);
        onChange(id, json);
        setSaveNeeded();
        setEditingIndex(null);
    };

    const handleCancel = () => {
        setEditingIndex(null);
        setFormError(null);
    };

    const handleFormChange = (field: string, fieldValue: string | boolean) => {
        if (field === 'name' && typeof fieldValue === 'string') {
            const sanitized = fieldValue.toLowerCase().replace(/[^a-z0-9-]/g, '');
            setEditForm((prev) => {
                if (prev.provider === 'nats') {
                    const nats = prev.nats || emptyNATSConfig;
                    const autoSubject = SUBJECT_PREFIX + prev.name;
                    const subjectIsAuto = nats.subject === '' || nats.subject === autoSubject || nats.subject === SUBJECT_PREFIX;
                    return {
                        ...prev,
                        name: sanitized,
                        nats: {
                            ...nats,
                            subject: subjectIsAuto ? SUBJECT_PREFIX + sanitized : nats.subject,
                        },
                    };
                }
                return {...prev, name: sanitized};
            });
            return;
        }
        if (field === 'provider' && typeof fieldValue === 'string') {
            setEditForm((prev) => {
                const updated: Connection = {...prev, provider: fieldValue as ProviderType};
                if (fieldValue === 'nats') {
                    if (!prev.nats) {
                        updated.nats = {...emptyNATSConfig};
                    }
                    updated.azure_queue = undefined;
                    updated.azure_blob = undefined;
                    updated.azure_servicebus = undefined;
                }
                if (fieldValue === 'azure-queue') {
                    if (!prev.azure_queue) {
                        updated.azure_queue = {...emptyAzureQueueConfig};
                    }
                    updated.nats = undefined;
                    updated.azure_blob = undefined;
                    updated.azure_servicebus = undefined;
                }
                if (fieldValue === 'azure-blob') {
                    if (!prev.azure_blob) {
                        updated.azure_blob = {...emptyAzureBlobConfig};
                    }
                    updated.nats = undefined;
                    updated.azure_queue = undefined;
                    updated.azure_servicebus = undefined;
                }
                if (fieldValue === 'azure-servicebus') {
                    if (!prev.azure_servicebus) {
                        updated.azure_servicebus = {...emptyAzureServiceBusConfig};
                    }
                    updated.nats = undefined;
                    updated.azure_queue = undefined;
                    updated.azure_blob = undefined;
                }
                return updated;
            });
            return;
        }
        setEditForm((prev) => ({...prev, [field]: fieldValue}));
    };

    const handleNATSChange = (field: keyof NATSProviderConfig, fieldValue: string | boolean) => {
        setEditForm((prev) => ({
            ...prev,
            nats: {...(prev.nats || emptyNATSConfig), [field]: fieldValue},
        }));
    };

    const handleAzureQueueChange = (field: keyof AzureQueueProviderConfig, fieldValue: string) => {
        setEditForm((prev) => ({
            ...prev,
            azure_queue: {...(prev.azure_queue || emptyAzureQueueConfig), [field]: fieldValue},
        }));
    };

    const handleAzureBlobChange = (field: keyof AzureBlobProviderConfig, fieldValue: string | number | undefined) => {
        setEditForm((prev) => ({
            ...prev,
            azure_blob: {...(prev.azure_blob || emptyAzureBlobConfig), [field]: fieldValue},
        }));
    };

    const handleAzureServiceBusChange = (field: keyof AzureServiceBusProviderConfig, fieldValue: string) => {
        setEditForm((prev) => ({
            ...prev,
            azure_servicebus: {...(prev.azure_servicebus || emptyAzureServiceBusConfig), [field]: fieldValue},
        }));
    };

    const handleTestConnection = async (index: number) => {
        setTestStatus((prev) => ({...prev, [index]: {loading: true}}));

        const direction = isInbound ? 'inbound' : 'outbound';

        try {
            const response = await fetch(`/plugins/${manifest.id}/api/v1/test-connection?direction=${direction}`, {
                method: 'POST',
                credentials: 'same-origin',
                headers: {
                    'Content-Type': 'application/json',
                    'X-CSRF-Token': getCSRFToken(),
                    'X-Requested-With': 'XMLHttpRequest',
                },
                body: JSON.stringify(connections[index]),
            });

            if (response.ok) {
                const data = await response.json();
                setTestStatus((prev) => ({
                    ...prev,
                    [index]: {
                        loading: false,
                        success: true,
                        message: data.message || 'Connection successful',
                    },
                }));
                setTimeout(() => {
                    setTestStatus((prev) => {
                        const next = {...prev};
                        delete next[index];
                        return next;
                    });
                }, TEST_STATUS_DISPLAY_MS);
            } else {
                let errorMessage = 'Connection failed';
                try {
                    const errorData = await response.json();
                    errorMessage = errorData.error || errorMessage;
                } catch {
                    // Use default error message
                }
                setTestStatus((prev) => ({
                    ...prev,
                    [index]: {loading: false, success: false, message: errorMessage},
                }));
                setTimeout(() => {
                    setTestStatus((prev) => {
                        const next = {...prev};
                        delete next[index];
                        return next;
                    });
                }, TEST_STATUS_DISPLAY_MS);
            }
        } catch (err) {
            const message = err instanceof Error ? err.message : 'Network error';
            setTestStatus((prev) => ({
                ...prev,
                [index]: {loading: false, success: false, message},
            }));
            setTimeout(() => {
                setTestStatus((prev) => {
                    const next = {...prev};
                    delete next[index];
                    return next;
                });
            }, TEST_STATUS_DISPLAY_MS);
        }
    };

    const renderForm = () => {
        const isEditing = editingIndex !== null && editingIndex >= 0;

        return (
            <div style={styles.form}>
                <div style={styles.formTitle}>
                    {isEditing ? 'Edit Connection' : 'New Connection'}
                </div>

                {formError && (
                    <div style={styles.formError}>
                        {formError}
                    </div>
                )}

                <div style={styles.formSection}>
                    <div style={styles.formSectionTitle as React.CSSProperties}>{'Connection'}</div>
                    <div style={styles.formRow}>
                        <div style={styles.inputGroup}>
                            <label style={styles.label}>{'Name'}</label>
                            <input
                                style={styles.input}
                                type='text'
                                value={editForm.name}
                                onChange={(e) => handleFormChange('name', e.target.value)}
                                disabled={disabled}
                                placeholder='my-connection'
                            />
                            <div style={styles.helpText}>
                                {'Lowercase letters, numbers, and hyphens only.'}
                            </div>
                        </div>
                        <div style={styles.inputGroup}>
                            <label style={styles.label}>{'Provider'}</label>
                            <select
                                style={styles.select}
                                value={editForm.provider}
                                onChange={(e) => handleFormChange('provider', e.target.value)}
                                disabled={disabled}
                            >
                                <option value='nats'>{'NATS'}</option>
                                <option value='azure-queue'>{'Azure Queue Storage'}</option>
                                <option value='azure-blob'>{'Azure Blob Storage (Batched)'}</option>
                                <option value='azure-servicebus'>{'Azure Service Bus'}</option>
                            </select>
                        </div>
                    </div>

                    {editForm.provider === 'nats' && (
                        <>
                            <div style={styles.formRow}>
                                <div style={styles.inputGroup}>
                                    <label style={styles.label}>{'Address'}</label>
                                    <input
                                        style={styles.input}
                                        type='text'
                                        value={editForm.nats?.address || ''}
                                        onChange={(e) => handleNATSChange('address', e.target.value)}
                                        disabled={disabled}
                                        placeholder={DEFAULT_NATS_ADDRESS}
                                    />
                                </div>
                                <div style={styles.inputGroup}>
                                    <label style={styles.label}>{'Subject'}</label>
                                    <input
                                        style={styles.input}
                                        type='text'
                                        value={editForm.nats?.subject || SUBJECT_PREFIX}
                                        onChange={(e) => handleNATSChange('subject', e.target.value)}
                                        disabled={disabled}
                                        placeholder={SUBJECT_PREFIX + 'my-connection'}
                                    />
                                    <div style={styles.helpText}>
                                        {`Defaults from connection name. Must start with "${SUBJECT_PREFIX}".`}
                                    </div>
                                </div>
                            </div>
                        </>
                    )}

                    {editForm.provider === 'azure-queue' && (() => {
                        const qAuthMode = editForm.azure_queue?.auth_mode || AZURE_AUTH_SHARED_KEY;
                        const qIsSP = qAuthMode === AZURE_AUTH_SERVICE_PRINCIPAL;
                        return (
                            <>
                                <div style={styles.formRow}>
                                    <div style={styles.inputGroup}>
                                        <label style={styles.label}>{'Auth Mode'}</label>
                                        <select
                                            aria-label='Azure Queue Auth Mode'
                                            style={styles.select}
                                            value={qAuthMode}
                                            onChange={(e) => handleAzureQueueChange('auth_mode', e.target.value)}
                                            disabled={disabled}
                                        >
                                            <option value={AZURE_AUTH_SHARED_KEY}>{'Shared Key (storage account key)'}</option>
                                            <option value={AZURE_AUTH_SERVICE_PRINCIPAL}>{'Service Principal (Azure AD Client Secret)'}</option>
                                        </select>
                                    </div>
                                    <div style={styles.inputGroup}>
                                        <label style={styles.label}>{'Azure Cloud'}</label>
                                        <select
                                            aria-label='Azure Queue Cloud'
                                            style={styles.select}
                                            value={editForm.azure_queue?.azure_cloud || AZURE_CLOUD_PUBLIC}
                                            onChange={(e) => handleAzureQueueChange('azure_cloud', e.target.value)}
                                            disabled={disabled}
                                        >
                                            <option value={AZURE_CLOUD_PUBLIC}>{'Azure Public'}</option>
                                            <option value={AZURE_CLOUD_USGOV}>{'Azure US Government'}</option>
                                            <option value={AZURE_CLOUD_CHINA}>{'Azure China'}</option>
                                        </select>
                                    </div>
                                </div>
                                <div style={styles.inputGroup}>
                                    <label style={styles.label}>{'Queue Service URL'}</label>
                                    <input
                                        style={styles.input}
                                        type='text'
                                        value={editForm.azure_queue?.queue_service_url || ''}
                                        onChange={(e) => handleAzureQueueChange('queue_service_url', e.target.value)}
                                        disabled={disabled}
                                        placeholder='https://myaccount.queue.core.windows.net'
                                    />
                                    <div style={styles.helpText}>
                                        {'Azure Queue Storage service endpoint. For example, https://myaccount.queue.core.windows.net.'}
                                    </div>
                                </div>
                                <div style={styles.inputGroup}>
                                    <label style={styles.label}>{'Account Name'}</label>
                                    <input
                                        style={styles.input}
                                        type='text'
                                        value={editForm.azure_queue?.account_name || ''}
                                        onChange={(e) => handleAzureQueueChange('account_name', e.target.value)}
                                        disabled={disabled}
                                        placeholder='myaccount'
                                    />
                                </div>
                                {!qIsSP && (
                                    <div style={styles.inputGroup}>
                                        <label style={styles.label}>{'Account Key'}</label>
                                        <input
                                            style={styles.input}
                                            type='password'
                                            value={editForm.azure_queue?.account_key || ''}
                                            onChange={(e) => handleAzureQueueChange('account_key', e.target.value)}
                                            disabled={disabled}
                                            placeholder='Paste key from Azure portal'
                                        />
                                        <div style={styles.helpText}>
                                            {'Azure Storage account shared key.'}
                                        </div>
                                    </div>
                                )}
                                <div style={styles.inputGroup}>
                                    <label style={styles.label}>{'Queue Name'}</label>
                                    <input
                                        style={styles.input}
                                        type='text'
                                        value={editForm.azure_queue?.queue_name || ''}
                                        onChange={(e) => handleAzureQueueChange('queue_name', e.target.value)}
                                        disabled={disabled}
                                        placeholder='crossguard-messages'
                                    />
                                </div>
                                {qIsSP && (
                                    <ServicePrincipalFieldsComponent
                                        ariaPrefix='Azure Queue'
                                        cfg={editForm.azure_queue || emptyAzureQueueConfig}
                                        onChange={(field, value) => handleAzureQueueChange(field as keyof AzureQueueProviderConfig, value)}
                                        disabled={disabled}
                                    />
                                )}
                            </>
                        );
                    })()}

                    {editForm.provider === 'azure-servicebus' && (() => {
                        const sbAuthMode = editForm.azure_servicebus?.auth_mode || AZURE_AUTH_CONNECTION_STRING;
                        const sbIsSP = sbAuthMode === AZURE_AUTH_SERVICE_PRINCIPAL;
                        return (
                            <>
                                <div style={styles.formRow}>
                                    <div style={styles.inputGroup}>
                                        <label style={styles.label}>{'Auth Mode'}</label>
                                        <select
                                            aria-label='Azure Service Bus Auth Mode'
                                            style={styles.select}
                                            value={sbAuthMode}
                                            onChange={(e) => handleAzureServiceBusChange('auth_mode', e.target.value)}
                                            disabled={disabled}
                                        >
                                            <option value={AZURE_AUTH_CONNECTION_STRING}>{'Connection String (SAS)'}</option>
                                            <option value={AZURE_AUTH_SERVICE_PRINCIPAL}>{'Service Principal (Azure AD Client Secret)'}</option>
                                        </select>
                                    </div>
                                    <div style={styles.inputGroup}>
                                        <label style={styles.label}>{'Azure Cloud'}</label>
                                        <select
                                            aria-label='Azure Service Bus Cloud'
                                            style={styles.select}
                                            value={editForm.azure_servicebus?.azure_cloud || AZURE_CLOUD_PUBLIC}
                                            onChange={(e) => handleAzureServiceBusChange('azure_cloud', e.target.value)}
                                            disabled={disabled}
                                        >
                                            <option value={AZURE_CLOUD_PUBLIC}>{'Azure Public'}</option>
                                            <option value={AZURE_CLOUD_USGOV}>{'Azure US Government'}</option>
                                            <option value={AZURE_CLOUD_CHINA}>{'Azure China'}</option>
                                        </select>
                                    </div>
                                </div>
                                {!sbIsSP && (
                                <div style={styles.inputGroup}>
                                    <label style={styles.label}>{'Connection String'}</label>
                                    <input
                                        aria-label='Azure Service Bus Connection String'
                                        style={styles.input}
                                        type='password'
                                        value={editForm.azure_servicebus?.connection_string || ''}
                                        onChange={(e) => handleAzureServiceBusChange('connection_string', e.target.value)}
                                        disabled={disabled}
                                        placeholder='Endpoint=sb://<namespace>.servicebus.windows.net/;SharedAccessKeyName=…;SharedAccessKey=…'
                                    />
                                    <div style={styles.helpText}>
                                        {'Azure Service Bus SAS connection string. Treat as a secret.'}
                                    </div>
                                </div>
                            )}
                                {sbIsSP && (
                                <div style={styles.inputGroup}>
                                    <label style={styles.label}>{'Service Bus Namespace'}</label>
                                    <input
                                        aria-label='Azure Service Bus Namespace'
                                        style={styles.input}
                                        type='text'
                                        value={editForm.azure_servicebus?.service_bus_namespace || ''}
                                        onChange={(e) => handleAzureServiceBusChange('service_bus_namespace', e.target.value)}
                                        disabled={disabled}
                                        placeholder='myns.servicebus.windows.net'
                                    />
                                    <div style={styles.helpText}>
                                        {'Fully-qualified Service Bus namespace. Use the appropriate cloud suffix (e.g. .servicebus.usgovcloudapi.net for Azure US Government).'}
                                    </div>
                                </div>
                            )}
                                <div style={styles.inputGroup}>
                                    <label style={styles.label}>{'Queue Name'}</label>
                                    <input
                                        aria-label='Azure Service Bus Queue Name'
                                        style={styles.input}
                                        type='text'
                                        value={editForm.azure_servicebus?.queue_name || ''}
                                        onChange={(e) => handleAzureServiceBusChange('queue_name', e.target.value)}
                                        disabled={disabled}
                                        placeholder='crossguard-relay'
                                    />
                                    <div style={styles.helpText}>
                                        {'Pre-created Service Bus queue. The plugin does not auto-create queues — use the Azure portal or ARM to create it with your desired LockDuration and MaxDeliveryCount.'}
                                    </div>
                                </div>
                                {sbIsSP && (
                                <ServicePrincipalFieldsComponent
                                    ariaPrefix='Azure Service Bus'
                                    cfg={editForm.azure_servicebus || emptyAzureServiceBusConfig}
                                    onChange={(field, value) => handleAzureServiceBusChange(field as keyof AzureServiceBusProviderConfig, value)}
                                    disabled={disabled}
                                />
                            )}
                                {editForm.file_transfer_enabled && (
                                <>
                                    <div style={styles.helpText}>
                                        {sbIsSP ? 'Service Bus is message-only. File attachments flow through a separate Azure Blob Storage container. In service-principal mode, the blob sidecar inherits the parent SP credential; provide only the service URL and container name below.' : 'Service Bus is message-only. File attachments flow through a separate Azure Blob Storage container. The Blob credentials below are distinct from the Service Bus connection string above.'}
                                    </div>
                                    <div style={styles.inputGroup}>
                                        <label style={styles.label}>{'Blob Service URL'}</label>
                                        <input
                                            aria-label='Azure Service Bus Blob Service URL'
                                            style={styles.input}
                                            type='text'
                                            value={editForm.azure_servicebus?.blob_service_url || ''}
                                            onChange={(e) => handleAzureServiceBusChange('blob_service_url', e.target.value)}
                                            disabled={disabled}
                                            placeholder='https://myaccount.blob.core.windows.net'
                                        />
                                    </div>
                                    {!sbIsSP && (
                                        <>
                                            <div style={styles.inputGroup}>
                                                <label style={styles.label}>{'Blob Account Name'}</label>
                                                <input
                                                    aria-label='Azure Service Bus Blob Account Name'
                                                    style={styles.input}
                                                    type='text'
                                                    value={editForm.azure_servicebus?.blob_account_name || ''}
                                                    onChange={(e) => handleAzureServiceBusChange('blob_account_name', e.target.value)}
                                                    disabled={disabled}
                                                    placeholder='myaccount'
                                                />
                                            </div>
                                            <div style={styles.inputGroup}>
                                                <label style={styles.label}>{'Blob Account Key'}</label>
                                                <input
                                                    aria-label='Azure Service Bus Blob Account Key'
                                                    style={styles.input}
                                                    type='password'
                                                    value={editForm.azure_servicebus?.blob_account_key || ''}
                                                    onChange={(e) => handleAzureServiceBusChange('blob_account_key', e.target.value)}
                                                    disabled={disabled}
                                                    placeholder='Paste key from Azure portal'
                                                />
                                            </div>
                                        </>
                                    )}
                                    <div style={styles.inputGroup}>
                                        <label style={styles.label}>{'Blob Container Name'}</label>
                                        <input
                                            aria-label='Azure Service Bus Blob Container Name'
                                            style={styles.input}
                                            type='text'
                                            value={editForm.azure_servicebus?.blob_container_name || ''}
                                            onChange={(e) => handleAzureServiceBusChange('blob_container_name', e.target.value)}
                                            disabled={disabled}
                                            placeholder='crossguard-files'
                                        />
                                    </div>
                                </>
                            )}
                            </>
                        );
                    })()}

                    {editForm.provider === 'azure-blob' && (() => {
                        const bAuthMode = editForm.azure_blob?.auth_mode || AZURE_AUTH_SHARED_KEY;
                        const bIsSP = bAuthMode === AZURE_AUTH_SERVICE_PRINCIPAL;
                        return (
                            <>
                                <div style={styles.formRow}>
                                    <div style={styles.inputGroup}>
                                        <label style={styles.label}>{'Auth Mode'}</label>
                                        <select
                                            aria-label='Azure Blob Auth Mode'
                                            style={styles.select}
                                            value={bAuthMode}
                                            onChange={(e) => handleAzureBlobChange('auth_mode', e.target.value)}
                                            disabled={disabled}
                                        >
                                            <option value={AZURE_AUTH_SHARED_KEY}>{'Shared Key (storage account key)'}</option>
                                            <option value={AZURE_AUTH_SERVICE_PRINCIPAL}>{'Service Principal (Azure AD Client Secret)'}</option>
                                        </select>
                                    </div>
                                    <div style={styles.inputGroup}>
                                        <label style={styles.label}>{'Azure Cloud'}</label>
                                        <select
                                            aria-label='Azure Blob Cloud'
                                            style={styles.select}
                                            value={editForm.azure_blob?.azure_cloud || AZURE_CLOUD_PUBLIC}
                                            onChange={(e) => handleAzureBlobChange('azure_cloud', e.target.value)}
                                            disabled={disabled}
                                        >
                                            <option value={AZURE_CLOUD_PUBLIC}>{'Azure Public'}</option>
                                            <option value={AZURE_CLOUD_USGOV}>{'Azure US Government'}</option>
                                            <option value={AZURE_CLOUD_CHINA}>{'Azure China'}</option>
                                        </select>
                                    </div>
                                </div>
                                <div style={styles.inputGroup}>
                                    <label style={styles.label}>{'Service URL'}</label>
                                    <input
                                        aria-label='Azure Blob Service URL'
                                        style={styles.input}
                                        type='text'
                                        value={editForm.azure_blob?.service_url || ''}
                                        onChange={(e) => handleAzureBlobChange('service_url', e.target.value)}
                                        disabled={disabled}
                                        placeholder='https://myaccount.blob.core.windows.net'
                                    />
                                    <div style={styles.helpText}>
                                        {'Azure Blob Storage service endpoint. For example, https://myaccount.blob.core.windows.net.'}
                                    </div>
                                </div>
                                <div style={styles.inputGroup}>
                                    <label style={styles.label}>{'Account Name'}</label>
                                    <input
                                        aria-label='Azure Blob Account Name'
                                        style={styles.input}
                                        type='text'
                                        value={editForm.azure_blob?.account_name || ''}
                                        onChange={(e) => handleAzureBlobChange('account_name', e.target.value)}
                                        disabled={disabled}
                                        placeholder='myaccount'
                                    />
                                </div>
                                {!bIsSP && (
                                <div style={styles.inputGroup}>
                                    <label style={styles.label}>{'Account Key'}</label>
                                    <input
                                        aria-label='Azure Blob Account Key'
                                        style={styles.input}
                                        type='password'
                                        value={editForm.azure_blob?.account_key || ''}
                                        onChange={(e) => handleAzureBlobChange('account_key', e.target.value)}
                                        disabled={disabled}
                                        placeholder='Paste key from Azure portal'
                                    />
                                    <div style={styles.helpText}>
                                        {'Azure Storage account shared key.'}
                                    </div>
                                </div>
                            )}
                                <div style={styles.inputGroup}>
                                    <label style={styles.label}>{'Blob Container Name'}</label>
                                    <input
                                        aria-label='Azure Blob Container Name'
                                        style={styles.input}
                                        type='text'
                                        value={editForm.azure_blob?.blob_container_name || ''}
                                        onChange={(e) => handleAzureBlobChange('blob_container_name', e.target.value)}
                                        disabled={disabled}
                                        placeholder='crossguard-batches'
                                    />
                                    <div style={styles.helpText}>
                                        {'Blob container used for both batched message files and deferred file attachments.'}
                                    </div>
                                </div>
                                {bIsSP && (
                                <ServicePrincipalFieldsComponent
                                    ariaPrefix='Azure Blob'
                                    cfg={editForm.azure_blob || emptyAzureBlobConfig}
                                    onChange={(field, value) => handleAzureBlobChange(field as keyof AzureBlobProviderConfig, value)}
                                    disabled={disabled}
                                />
                            )}
                                <div style={styles.inputGroup}>
                                    <label style={styles.label}>{'Flush Interval (seconds)'}</label>
                                    <input
                                        aria-label='Azure Blob Flush Interval in seconds'
                                        style={styles.input}
                                        type='number'
                                        min={5}
                                        step={1}
                                        value={editForm.azure_blob?.flush_interval_seconds ?? ''}
                                        onChange={(e) => {
                                        const raw = e.target.value;
                                        if (raw === '') {
                                            handleAzureBlobChange('flush_interval_seconds', undefined);
                                            return;
                                        }
                                        const parsed = parseInt(raw, 10);
                                        if (Number.isNaN(parsed)) {
                                            return;
                                        }
                                        handleAzureBlobChange('flush_interval_seconds', parsed);
                                    }}
                                        disabled={disabled}
                                    />
                                    <div style={styles.helpText}>
                                        {'How often batched message files are uploaded to blob storage. Default 60 seconds, minimum 5.'}
                                    </div>
                                </div>
                            </>
                        );
                    })()}

                    {!isInbound && (
                        <div style={styles.inputGroup}>
                            <label style={styles.label}>{'Message Format'}</label>
                            <select
                                style={styles.select}
                                value={editForm.message_format}
                                onChange={(e) => handleFormChange('message_format', e.target.value)}
                                disabled={disabled}
                            >
                                <option value='json'>{'JSON'}</option>
                                <option value='xml'>{'XML (for Cross Domain Solutions)'}</option>
                            </select>
                            <div style={styles.helpText}>
                                {'Wire format for outbound messages. Use XML when sending through a Cross Domain Solution. Inbound messages are auto-detected.'}
                            </div>
                        </div>
                    )}
                </div>

                {editForm.provider === 'nats' && (
                    <>
                        <div style={styles.formSection}>
                            <div style={styles.formSectionTitle as React.CSSProperties}>{'Authentication'}</div>
                            <div style={styles.inputGroup}>
                                <label style={styles.label}>{'Auth Type'}</label>
                                <select
                                    style={styles.select}
                                    value={editForm.nats?.auth_type || 'none'}
                                    onChange={(e) => handleNATSChange('auth_type', e.target.value)}
                                    disabled={disabled}
                                >
                                    <option value='none'>{'None'}</option>
                                    <option value='token'>{'Token'}</option>
                                    <option value='credentials'>{'Username / Password'}</option>
                                </select>
                            </div>
                            {editForm.nats?.auth_type === 'token' && (
                                <div style={styles.inputGroup}>
                                    <label style={styles.label}>{'Token'}</label>
                                    <input
                                        style={styles.input}
                                        type='password'
                                        value={editForm.nats?.token || ''}
                                        onChange={(e) => handleNATSChange('token', e.target.value)}
                                        disabled={disabled}
                                        placeholder='Enter token'
                                    />
                                </div>
                            )}
                            {editForm.nats?.auth_type === 'credentials' && (
                                <div style={styles.formRow}>
                                    <div style={styles.inputGroup}>
                                        <label style={styles.label}>{'Username'}</label>
                                        <input
                                            style={styles.input}
                                            type='text'
                                            value={editForm.nats?.username || ''}
                                            onChange={(e) => handleNATSChange('username', e.target.value)}
                                            disabled={disabled}
                                            placeholder='Enter username'
                                        />
                                    </div>
                                    <div style={styles.inputGroup}>
                                        <label style={styles.label}>{'Password'}</label>
                                        <input
                                            style={styles.input}
                                            type='password'
                                            value={editForm.nats?.password || ''}
                                            onChange={(e) => handleNATSChange('password', e.target.value)}
                                            disabled={disabled}
                                            placeholder='Enter password'
                                        />
                                    </div>
                                </div>
                            )}
                        </div>

                        <div style={styles.formSection}>
                            <div style={styles.formSectionTitle as React.CSSProperties}>{'Security'}</div>
                            <div style={styles.inputGroup}>
                                <label style={styles.checkbox}>
                                    <input
                                        type='checkbox'
                                        checked={editForm.nats?.tls_enabled || false}
                                        onChange={(e) => handleNATSChange('tls_enabled', e.target.checked)}
                                        disabled={disabled}
                                    />
                                    {'Enable TLS'}
                                </label>
                                <div style={styles.helpText}>
                                    {'Encrypt the connection to the NATS server using TLS.'}
                                </div>
                            </div>
                            {editForm.nats?.tls_enabled && (
                                <>
                                    <div style={styles.formRow}>
                                        <div style={styles.inputGroup}>
                                            <label style={styles.label}>{'Client Cert Path'}</label>
                                            <input
                                                style={styles.input}
                                                type='text'
                                                value={editForm.nats?.client_cert || ''}
                                                onChange={(e) => handleNATSChange('client_cert', e.target.value)}
                                                disabled={disabled}
                                                placeholder='/path/to/client.crt'
                                            />
                                        </div>
                                        <div style={styles.inputGroup}>
                                            <label style={styles.label}>{'Client Key Path'}</label>
                                            <input
                                                style={styles.input}
                                                type='text'
                                                value={editForm.nats?.client_key || ''}
                                                onChange={(e) => handleNATSChange('client_key', e.target.value)}
                                                disabled={disabled}
                                                placeholder='/path/to/client.key'
                                            />
                                        </div>
                                    </div>
                                    <div style={styles.inputGroup}>
                                        <label style={styles.label}>{'CA Cert Path'}</label>
                                        <input
                                            style={styles.input}
                                            type='text'
                                            value={editForm.nats?.ca_cert || ''}
                                            onChange={(e) => handleNATSChange('ca_cert', e.target.value)}
                                            disabled={disabled}
                                            placeholder='/path/to/ca.crt'
                                        />
                                    </div>
                                </>
                            )}
                        </div>
                    </>
                )}

                <div style={styles.formSection}>
                    <div style={styles.formSectionTitle as React.CSSProperties}>{'File Transfer'}</div>
                    <div style={styles.inputGroup}>
                        <label style={styles.checkbox}>
                            <input
                                type='checkbox'
                                checked={editForm.file_transfer_enabled}
                                onChange={(e) => handleFormChange('file_transfer_enabled', e.target.checked)}
                                disabled={disabled}
                            />
                            {'Enable File Transfer'}
                        </label>
                        <div style={styles.helpText}>
                            {editForm.provider === 'nats' && 'Relay file attachments on posts across this connection. Requires JetStream on the NATS server.'}
                            {editForm.provider === 'azure-queue' && 'Relay file attachments on posts across this connection. Files are stored in Azure Blob Storage.'}
                            {editForm.provider === 'azure-blob' && 'Relay file attachments on posts across this connection. Files are deferred and uploaded after each message batch flush.'}
                            {editForm.provider === 'azure-servicebus' && 'Relay file attachments on posts across this connection. Service Bus is message-only, so files flow through a separate Azure Blob Storage container (configured with its own credentials below).'}
                        </div>
                    </div>
                    {editForm.file_transfer_enabled && editForm.provider === 'azure-queue' && (
                        <>
                            <div style={styles.inputGroup}>
                                <label style={styles.label}>{'Blob Service URL'}</label>
                                <input
                                    aria-label='Azure Queue Blob Service URL'
                                    style={styles.input}
                                    type='text'
                                    value={editForm.azure_queue?.blob_service_url || ''}
                                    onChange={(e) => handleAzureQueueChange('blob_service_url', e.target.value)}
                                    disabled={disabled}
                                    placeholder='https://myaccount.blob.core.windows.net'
                                />
                                <div style={styles.helpText}>
                                    {'Azure Blob Storage service endpoint. For example, https://myaccount.blob.core.windows.net.'}
                                </div>
                            </div>
                            <div style={styles.inputGroup}>
                                <label style={styles.label}>{'Blob Container Name'}</label>
                                <input
                                    style={styles.input}
                                    type='text'
                                    value={editForm.azure_queue?.blob_container_name || ''}
                                    onChange={(e) => handleAzureQueueChange('blob_container_name', e.target.value)}
                                    disabled={disabled}
                                    placeholder='crossguard-files'
                                />
                                <div style={styles.helpText}>
                                    {'Azure Blob Storage container for file attachments.'}
                                </div>
                            </div>
                        </>
                    )}
                    {editForm.file_transfer_enabled && (
                        <>
                            <div style={styles.inputGroup}>
                                <label style={styles.label}>{'File Filter Mode'}</label>
                                <select
                                    style={styles.select}
                                    value={editForm.file_filter_mode}
                                    onChange={(e) => handleFormChange('file_filter_mode', e.target.value)}
                                    disabled={disabled}
                                >
                                    <option value=''>{'None (all types allowed)'}</option>
                                    <option value='allow'>{'Allow only these types'}</option>
                                    <option value='deny'>{'Block these types'}</option>
                                </select>
                            </div>
                            {(editForm.file_filter_mode === 'allow' || editForm.file_filter_mode === 'deny') && (
                                <div style={styles.inputGroup}>
                                    <label style={styles.label}>{'File Types'}</label>
                                    <input
                                        style={styles.input}
                                        type='text'
                                        value={editForm.file_filter_types}
                                        onChange={(e) => handleFormChange('file_filter_types', e.target.value)}
                                        disabled={disabled}
                                        placeholder='.pdf,.docx,.png,.jpg'
                                    />
                                    <div style={styles.helpText}>
                                        {'Comma-separated list of file extensions.'}
                                    </div>
                                </div>
                            )}
                        </>
                    )}
                </div>

                <div style={styles.formActions}>
                    <button
                        style={styles.btnPrimary}
                        onClick={handleSave}
                        disabled={disabled}
                    >
                        {isEditing ? 'Update Connection' : 'Add Connection'}
                    </button>
                    <button
                        style={styles.btnSecondary}
                        onClick={handleCancel}
                    >
                        {'Cancel'}
                    </button>
                </div>
            </div>
        );
    };

    const renderCard = (conn: Connection, index: number) => {
        if (editingIndex === index) {
            return (
                <div key={conn.name || `editing-${index}`}>
                    {renderForm()}
                </div>
            );
        }

        const status = testStatus[index];
        let providerLabel;
        switch (conn.provider) {
        case 'nats':
            providerLabel = 'NATS';
            break;
        case 'azure-queue':
            providerLabel = 'Azure Queue';
            break;
        case 'azure-blob':
            providerLabel = 'Azure Blob';
            break;
        case 'azure-servicebus':
            providerLabel = 'Azure Service Bus';
            break;
        default:
            providerLabel = 'Unknown';
            break;
        }

        return (
            <div
                key={conn.name || `card-${index}`}
                style={styles.card}
            >
                <div style={styles.cardHeader}>
                    <div style={styles.cardTitle}>
                        {conn.name}
                        <span style={{...styles.badge, ...styles.badgeAuth}}>
                            {providerLabel}
                        </span>
                        {conn.provider === 'nats' && (
                            <span style={{...styles.badge, ...styles.badgeAuth}}>
                                {authLabel(conn.nats?.auth_type || 'none')}
                            </span>
                        )}
                        {conn.provider === 'nats' && conn.nats?.tls_enabled && (
                            <span style={{...styles.badge, ...styles.badgeTls}}>
                                {'TLS'}
                            </span>
                        )}
                        {conn.file_transfer_enabled && (
                            <span style={{...styles.badge, ...styles.badgeTls}}>
                                {'Files'}
                            </span>
                        )}
                        {!isInbound && conn.message_format === 'xml' && (
                            <span style={{...styles.badge, ...styles.badgeTls}}>
                                {'XML'}
                            </span>
                        )}
                    </div>
                </div>
                <div style={styles.cardMeta}>
                    {conn.provider === 'nats' && (
                        <>
                            <div style={styles.cardMetaItem}>
                                <span style={styles.cardMetaLabel}>{'Address'}</span>
                                {conn.nats?.address}
                            </div>
                            <div style={styles.cardMetaItem}>
                                <span style={styles.cardMetaLabel}>{'Subject'}</span>
                                {conn.nats?.subject}
                            </div>
                        </>
                    )}
                    {conn.provider === 'azure-queue' && (
                        <div style={styles.cardMetaItem}>
                            <span style={styles.cardMetaLabel}>{'Queue'}</span>
                            {conn.azure_queue?.queue_name}
                        </div>
                    )}
                    {conn.provider === 'azure-servicebus' && (
                        <div style={styles.cardMetaItem}>
                            <span style={styles.cardMetaLabel}>{'Queue'}</span>
                            {conn.azure_servicebus?.queue_name}
                        </div>
                    )}
                    {conn.provider === 'azure-blob' && (
                        <div style={styles.cardMetaItem}>
                            <span style={styles.cardMetaLabel}>{'Container'}</span>
                            {conn.azure_blob?.blob_container_name}
                        </div>
                    )}
                    {conn.file_transfer_enabled && (
                        <div style={styles.cardMetaItem}>
                            <span style={styles.cardMetaLabel}>{'Files'}</span>
                            {conn.file_filter_mode === 'allow' && `Allow: ${conn.file_filter_types}`}
                            {conn.file_filter_mode === 'deny' && `Deny: ${conn.file_filter_types}`}
                            {conn.file_filter_mode === '' && 'All types allowed'}
                        </div>
                    )}
                    {!isInbound && conn.message_format === 'xml' && (
                        <div style={styles.cardMetaItem}>
                            <span style={styles.cardMetaLabel}>{'Format'}</span>
                            {'XML'}
                        </div>
                    )}
                </div>
                <div style={styles.cardActions}>
                    <button
                        style={{...styles.btnSecondary, ...styles.btnSmall}}
                        onClick={() => handleEdit(index)}
                        disabled={disabled || editingIndex !== null}
                    >
                        {'Edit'}
                    </button>
                    <button
                        style={{...styles.btnSecondary, ...styles.btnSmall}}
                        onClick={() => handleTestConnection(index)}
                        disabled={disabled || (status?.loading ?? false) || editingIndex !== null}
                    >
                        {status?.loading ? 'Testing...' : 'Test Connection'}
                    </button>
                    <div style={{flex: 1}}/>
                    <button
                        style={{...styles.btnDanger, ...styles.btnSmall}}
                        onClick={() => handleDelete(index)}
                        disabled={disabled || editingIndex !== null}
                    >
                        {'Remove'}
                    </button>
                </div>
                {status && !status.loading && status.success !== undefined && (
                    <div
                        style={{
                        ...styles.statusBanner,
                        ...(status.success ? styles.statusSuccess : styles.statusError),
                    }}
                    >
                        {status.message}
                    </div>
                )}
            </div>
        );
    };

    const sectionTitle = isInbound ? 'Inbound' : 'Outbound';
    const sectionDesc = isInbound ? 'Messages received from external providers and relayed into Mattermost.' : 'Messages sent from Mattermost to external providers.';
    const directionStyle = isInbound ? styles.directionInbound : styles.directionOutbound;

    return (
        <div style={styles.container}>
            <div style={styles.sectionHeader}>
                <div style={styles.sectionTitle as React.CSSProperties}>
                    {sectionTitle}
                    <span style={{...styles.directionBadge, ...directionStyle} as React.CSSProperties}>
                        {isInbound ? 'Provider \u2192 Mattermost' : 'Mattermost \u2192 Provider'}
                    </span>
                </div>
                <p style={styles.sectionDesc}>{sectionDesc}</p>
            </div>
            <div style={styles.addArea}>
                <button
                    style={styles.btnPrimary}
                    onClick={handleAdd}
                    disabled={disabled || editingIndex !== null}
                >
                    {'+ Add Connection'}
                </button>
            </div>
            {editingIndex === -1 && renderForm()}
            {connections.length === 0 && editingIndex === null && (
                <div style={styles.emptyState as React.CSSProperties}>
                    <div style={{fontSize: '15px', fontWeight: 600, marginBottom: '4px', color: colors.textMuted}}>
                        {'No connections configured'}
                    </div>
                    <div>
                        {'Click '}
                        <strong>{'+ Add Connection'}</strong>
                        {' to get started.'}
                    </div>
                </div>
            )}
            {connections.map((conn, index) => renderCard(conn, index))}
        </div>
    );
};

export default ConnectionSettings;
