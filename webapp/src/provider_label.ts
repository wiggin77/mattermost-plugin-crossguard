export function providerLabel(provider?: string): string {
    switch (provider) {
    case 'nats':
        return 'NATS';
    case 'azure-queue':
        return 'AZURE QUEUE';
    case 'azure-blob':
        return 'AZURE BLOB';
    case 'azure-servicebus':
        return 'AZURE SERVICE BUS';
    default:
        // Empty/undefined provider typically means the connection is
        // orphaned (no longer in the plugin config), so the server could
        // not look up its type. Display a neutral label rather than
        // defaulting to "NATS", which would be misleading.
        return 'UNKNOWN';
    }
}
