import { describe, expect, it } from 'vitest';
import { sanitizeHtml } from '@/lib/modals';

// An object's name reaches a dialog's HTML as written: only the formatting the
// translations use stays.
describe('sanitizeHtml', () => {
  it('keeps the formatting of the translations', () => {
    expect(sanitizeHtml('Mode is <strong>immutable</strong> (current: <mono>COMPLIANCE</mono>)')).toBe(
      'Mode is <strong>immutable</strong> (current: <mono>COMPLIANCE</mono>)',
    );
    expect(sanitizeHtml('<code class="font-mono">s3:GetObject</code>')).toBe('<code class="font-mono">s3:GetObject</code>');
    expect(sanitizeHtml('<a href="https://docs.maxiofs.com/">docs</a>')).toBe('<a href="https://docs.maxiofs.com/">docs</a>');
  });

  it('drops what loads, runs or takes input', () => {
    for (const attack of [
      '<img src=x onerror=alert(1)>',
      '<script>alert(1)</script>',
      '<svg><a xlink:href="javascript:alert(1)"><text>x</text></a></svg>',
      '<input name=password placeholder="Password">',
      '<iframe src="https://evil.example"></iframe>',
      '<noscript><p title="</noscript><img src=x onerror=alert(1)>"></noscript>',
      '<math><mi><a href="javascript:alert(1)">x</a></mi></math>',
    ]) {
      const out = sanitizeHtml(`Delete <strong>${attack}</strong>?`);
      expect(out).not.toMatch(/<(img|script|svg|input|iframe|noscript|math)\b/i);
      expect(out).not.toMatch(/onerror|javascript:/i);
    }
  });

  it('keeps the text of other tags and no attribute but a class and a safe link', () => {
    expect(sanitizeHtml('<div style="position:fixed">over</div><b onclick="x()" class="c">bold</b>')).toBe(
      'over<b class="c">bold</b>',
    );
    expect(sanitizeHtml('<a href="javascript:alert(1)" target="_blank">x</a>')).toBe('<a>x</a>');
    expect(sanitizeHtml('<a href="&#106;avascript:alert(1)">x</a>')).toBe('<a>x</a>');
  });
});
