import { describe, expect, it } from 'vitest';
import { decodeJWTPayload, extractTokenPair, unwrapAPIData } from '@/lib/api';

function base64url(value: string): string {
  return btoa(value).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '');
}

describe('JWT payload decoding', () => {
  it('decodes base64url JWT payloads without padding', () => {
    const payload = { sub: 'user-1', exp: 1893456000, token_type: 'access' };
    const token = [
      base64url(JSON.stringify({ alg: 'HS256', typ: 'JWT' })),
      base64url(JSON.stringify(payload)),
      'signature',
    ].join('.');

    expect(decodeJWTPayload(token)).toEqual(payload);
  });

  it('returns null for malformed tokens', () => {
    expect(decodeJWTPayload('not-a-jwt')).toBeNull();
  });
});

describe('API response normalization', () => {
  it('extracts refresh token pairs from wrapped Console API responses', () => {
    expect(extractTokenPair({
      success: true,
      data: {
        access_token: 'access-1',
        refresh_token: 'refresh-1',
      },
    })).toEqual({
      access_token: 'access-1',
      refresh_token: 'refresh-1',
    });
  });

  it('keeps plain refresh token pair responses compatible', () => {
    expect(extractTokenPair({
      access_token: 'access-2',
      refresh_token: 'refresh-2',
    })).toEqual({
      access_token: 'access-2',
      refresh_token: 'refresh-2',
    });
  });

  it('treats wrapped refresh responses without data as missing tokens', () => {
    expect(extractTokenPair({
      success: true,
      data: undefined,
    })).toEqual({});
  });

  it('unwraps object tag payloads returned by writeJSON', () => {
    const tags = unwrapAPIData({
      success: true,
      data: {
        tags: [
          { key: 'env', value: 'prod' },
        ],
      },
    });

    expect(tags.tags).toEqual([{ key: 'env', value: 'prod' }]);
  });

  it('unwraps group list payloads returned by writeJSON', () => {
    const payload = unwrapAPIData({
      success: true,
      data: {
        groups: [
          { id: 'group-1', name: 'ops', displayName: 'Ops', tenantId: '' },
        ],
        total: 1,
      },
    });

    expect(payload.groups).toHaveLength(1);
    expect(payload.groups[0].name).toBe('ops');
  });

  it('unwraps group member list payloads returned by writeJSON', () => {
    const payload = unwrapAPIData({
      success: true,
      data: {
        members: [
          { groupId: 'group-1', userId: 'user-1', addedBy: 'admin-1' },
        ],
        total: 1,
      },
    });

    expect(payload.members).toEqual([
      { groupId: 'group-1', userId: 'user-1', addedBy: 'admin-1' },
    ]);
  });

  it('unwraps object helper payloads returned by writeJSON', () => {
    const rename = unwrapAPIData({
      success: true,
      data: { newKey: 'archive/report.txt' },
    });
    const folderSize = unwrapAPIData({
      success: true,
      data: { size: 4096, count: 2 },
    });

    expect(rename.newKey).toBe('archive/report.txt');
    expect(folderSize).toEqual({ size: 4096, count: 2 });
  });
});
