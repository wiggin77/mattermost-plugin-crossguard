import {test, expect} from '@playwright/test';

import {providerLabel} from './provider_label';

test.describe('providerLabel', () => {
    test('returns AZURE QUEUE for azure-queue provider', () => {
        expect(providerLabel('azure-queue')).toBe('AZURE QUEUE');
    });

    test('returns AZURE BLOB for azure-blob provider', () => {
        expect(providerLabel('azure-blob')).toBe('AZURE BLOB');
    });

    test('returns NATS for nats provider', () => {
        expect(providerLabel('nats')).toBe('NATS');
    });

    test('returns NATS when provider is undefined', () => {
        expect(providerLabel(undefined)).toBe('NATS');
    });

    test('returns NATS when provider is an empty string', () => {
        expect(providerLabel('')).toBe('NATS');
    });

    test('returns NATS for an unknown provider value', () => {
        expect(providerLabel('unknown-provider')).toBe('NATS');
    });
});
