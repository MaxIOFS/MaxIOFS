import { describe, expect, it } from 'vitest';
import { decodeJWTPayload } from '@/lib/api';

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
