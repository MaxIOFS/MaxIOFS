import { describe, it, expect } from 'vitest';
import { lifecycleXML, classMovesOf, consoleRuleOf, formOf, noMove, CONSOLE_RULE_ID } from '@/lib/lifecycleRules';

function parse(xml: string): Document {
  return new DOMParser().parseFromString(xml, 'application/xml');
}

describe('lifecycle rules of the bucket settings', () => {
  it('expires noncurrent versions and lone delete markers', () => {
    const doc = parse(lifecycleXML({ noncurrentDays: 30, deleteExpiredMarkers: true, moveCurrent: noMove, moveNoncurrent: noMove }));
    expect(doc.querySelector('parsererror')).toBeNull();
    expect(doc.querySelector('Rule > NoncurrentVersionExpiration > NoncurrentDays')?.textContent).toBe('30');
    expect(doc.querySelector('Rule > Expiration > ExpiredObjectDeleteMarker')?.textContent).toBe('true');
    expect(doc.querySelector('Transition')).toBeNull();
    expect(doc.querySelector('NoncurrentVersionTransition')).toBeNull();

    const none = parse(lifecycleXML({ noncurrentDays: 7, deleteExpiredMarkers: false, moveCurrent: noMove, moveNoncurrent: noMove }));
    expect(none.querySelector('Expiration')).toBeNull();
  });

  it('moves objects and noncurrent versions to a colder class', () => {
    const doc = parse(lifecycleXML({
      noncurrentDays: 90,
      deleteExpiredMarkers: false,
      moveCurrent: { on: true, days: 30, storageClass: 'GLACIER' },
      moveNoncurrent: { on: true, days: 0, storageClass: 'DEEP_ARCHIVE' },
    }));
    expect(doc.querySelector('parsererror')).toBeNull();
    expect(doc.querySelector('Rule > Transition > Days')?.textContent).toBe('30');
    expect(doc.querySelector('Rule > Transition > StorageClass')?.textContent).toBe('GLACIER');
    expect(doc.querySelector('Rule > NoncurrentVersionTransition > NoncurrentDays')?.textContent).toBe('0');
    expect(doc.querySelector('Rule > NoncurrentVersionTransition > StorageClass')?.textContent).toBe('DEEP_ARCHIVE');
  });

  it('leaves out a move turned off or of no class', () => {
    const doc = parse(lifecycleXML({
      noncurrentDays: 30,
      deleteExpiredMarkers: true,
      moveCurrent: { on: false, days: 30, storageClass: 'GLACIER' },
      moveNoncurrent: { on: true, days: 10, storageClass: '' },
    }));
    expect(doc.querySelector('Transition')).toBeNull();
    expect(doc.querySelector('NoncurrentVersionTransition')).toBeNull();
  });

  it('writes the rule of the console by its ID', () => {
    const doc = parse(lifecycleXML({ noncurrentDays: 30, deleteExpiredMarkers: true, moveCurrent: noMove, moveNoncurrent: noMove }));
    expect(doc.querySelectorAll('Rule')).toHaveLength(1);
    expect(doc.querySelector('Rule > ID')?.textContent).toBe(CONSOLE_RULE_ID);
  });

  it('finds the rule of the console among those set through the S3 API', () => {
    const aging = { ID: 'aging', Status: 'Enabled', Filter: { Prefix: 'aging/' } };
    const own = { ID: CONSOLE_RULE_ID, Status: 'Enabled', NoncurrentVersionExpiration: { NoncurrentDays: 60 } };
    expect(consoleRuleOf(undefined)).toEqual({ rule: undefined, others: 0 });
    expect(consoleRuleOf([aging])).toEqual({ rule: undefined, others: 1 });
    expect(consoleRuleOf([aging, own, { ID: '', Status: 'Enabled' }])).toEqual({ rule: own, others: 2 });
  });

  it('asks the values of the rule of the console, or the defaults with none', () => {
    expect(formOf(undefined)).toEqual({ noncurrentDays: 30, deleteExpiredMarkers: true, moveCurrent: noMove, moveNoncurrent: noMove });
    expect(formOf({ ID: CONSOLE_RULE_ID, Status: 'Enabled', NoncurrentVersionExpiration: { NoncurrentDays: 7 } }))
      .toEqual({ noncurrentDays: 7, deleteExpiredMarkers: false, moveCurrent: noMove, moveNoncurrent: noMove });
    expect(formOf({
      ID: CONSOLE_RULE_ID,
      Status: 'Enabled',
      NoncurrentVersionExpiration: { NoncurrentDays: 90 },
      Expiration: { ExpiredObjectDeleteMarker: true },
      Transitions: [{ Days: 10, StorageClass: 'GLACIER' }],
    })).toEqual({ noncurrentDays: 90, deleteExpiredMarkers: true, moveCurrent: { on: true, days: 10, storageClass: 'GLACIER' }, moveNoncurrent: noMove });
  });

  it('reads the moves of a stored rule', () => {
    expect(classMovesOf(undefined)).toEqual({ moveCurrent: noMove, moveNoncurrent: noMove });
    expect(classMovesOf({
      ID: 'r',
      Status: 'Enabled',
      Transitions: [{ Date: '2027-01-01T00:00:00Z', StorageClass: 'GLACIER_IR' }, { Days: 60, StorageClass: 'GLACIER' }],
      NoncurrentVersionTransitions: [{ NoncurrentDays: 5, StorageClass: 'DEEP_ARCHIVE' }],
    })).toEqual({
      moveCurrent: { on: true, days: 60, storageClass: 'GLACIER' },
      moveNoncurrent: { on: true, days: 5, storageClass: 'DEEP_ARCHIVE' },
    });
  });
});
