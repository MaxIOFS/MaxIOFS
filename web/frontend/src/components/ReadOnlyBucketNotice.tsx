import { Eye } from 'lucide-react';

// A global administrator sees every tenant's buckets but cannot change them,
// the same way an AWS account cannot write into another account's bucket. The
// individual controls are disabled; this says why, once, at the top.
export function ReadOnlyBucketNotice({ message }: { message: string }) {
  return (
    <div
      role="status"
      className="flex items-start gap-3 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 dark:border-amber-800 dark:bg-amber-900/20"
    >
      <Eye className="mt-0.5 h-4 w-4 shrink-0 text-amber-600 dark:text-amber-400" />
      <p className="text-sm text-amber-800 dark:text-amber-300">{message}</p>
    </div>
  );
}
