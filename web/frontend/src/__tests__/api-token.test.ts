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
});
