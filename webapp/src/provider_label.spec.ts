import {test, expect} from '@playwright/test';

import {providerLabel} from './provider_label';

test.describe('providerLabel', () => {
    test('returns AZURE QUEUE for azure-queue provider', () => {
        expect(providerLabel('azure-queue')).toBe('AZURE QUEUE');
    });

    test('returns AZURE BLOB for azure-blob provider', () => {
        expect(providerLabel('azure-blob')).toBe('AZURE BLOB');
    });

    test('returns AZURE SERVICE BUS for azure-servicebus provider', () => {
        expect(providerLabel('azure-servicebus')).toBe('AZURE SERVICE BUS');
    });

    test('returns NATS for nats provider', () => {
        expect(providerLabel('nats')).toBe('NATS');
    });

    test('returns UNKNOWN when provider is undefined', () => {
        // Typically indicates an orphaned connection (no longer in config).
        expect(providerLabel(undefined)).toBe('UNKNOWN');
    });

    test('returns UNKNOWN when provider is an empty string', () => {
        expect(providerLabel('')).toBe('UNKNOWN');
    });

    test('returns UNKNOWN for an unrecognized provider value', () => {
        expect(providerLabel('foo-bar')).toBe('UNKNOWN');
    });
});
