import type { LifecycleRule } from '@/types';

// ClassMove moves objects to a colder storage class days after they were
// written, or became noncurrent.
export type ClassMove = { on: boolean; days: number; storageClass: string };

export const noMove: ClassMove = { on: false, days: 30, storageClass: '' };

// The classes a lifecycle rule can move objects to.
export const TRANSITION_CLASSES = ['STANDARD_IA', 'INTELLIGENT_TIERING', 'ONEZONE_IA', 'GLACIER_IR', 'GLACIER', 'DEEP_ARCHIVE'];

// LifecycleForm is what the bucket settings ask of a lifecycle.
export interface LifecycleForm {
  noncurrentDays: number;
  deleteExpiredMarkers: boolean;
  moveCurrent: ClassMove;
  moveNoncurrent: ClassMove;
}

// lifecycleXML is the lifecycle configuration of the form: one rule for the
// whole bucket.
export function lifecycleXML(form: LifecycleForm): string {
  const parts = [
    `    <NoncurrentVersionExpiration>\n      <NoncurrentDays>${form.noncurrentDays}</NoncurrentDays>\n    </NoncurrentVersionExpiration>`,
  ];
  if (form.deleteExpiredMarkers) {
    parts.push('    <Expiration>\n      <ExpiredObjectDeleteMarker>true</ExpiredObjectDeleteMarker>\n    </Expiration>');
  }
  if (form.moveCurrent.on && form.moveCurrent.storageClass) {
    parts.push(`    <Transition>\n      <Days>${form.moveCurrent.days}</Days>\n      <StorageClass>${form.moveCurrent.storageClass}</StorageClass>\n    </Transition>`);
  }
  if (form.moveNoncurrent.on && form.moveNoncurrent.storageClass) {
    parts.push(`    <NoncurrentVersionTransition>\n      <NoncurrentDays>${form.moveNoncurrent.days}</NoncurrentDays>\n      <StorageClass>${form.moveNoncurrent.storageClass}</StorageClass>\n    </NoncurrentVersionTransition>`);
  }
  return `<LifecycleConfiguration>
  <Rule>
    <ID>delete-old-versions</ID>
    <Status>Enabled</Status>
    <Prefix></Prefix>
${parts.join('\n')}
  </Rule>
</LifecycleConfiguration>`;
}

// classMovesOf are the moves a stored rule makes: its first transition by
// days and its first noncurrent transition.
export function classMovesOf(rule?: LifecycleRule): { moveCurrent: ClassMove; moveNoncurrent: ClassMove } {
  const transition = rule?.Transitions?.find((tr) => tr.Days !== undefined);
  const noncurrent = rule?.NoncurrentVersionTransitions?.[0];
  return {
    moveCurrent: transition ? { on: true, days: transition.Days ?? 0, storageClass: transition.StorageClass } : noMove,
    moveNoncurrent: noncurrent ? { on: true, days: noncurrent.NoncurrentDays, storageClass: noncurrent.StorageClass } : noMove,
  };
}
