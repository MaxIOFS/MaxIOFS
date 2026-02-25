import React from 'react';
import { X, CheckCircle2, AlertCircle, Loader2 } from 'lucide-react';
import { useBgTaskStore, BgTask } from '@/lib/modals';
import { cn } from '@/lib/utils';

function TaskCard({ task, onRemove }: { task: BgTask; onRemove: () => void }) {
  const pct = task.total > 0 ? Math.min(100, Math.round((task.done / task.total) * 100)) : 0;
  const isRunning = task.status === 'running';
  const isError = task.status === 'error';
  const isDone = task.status === 'done';

  return (
    <div className="bg-white dark:bg-gray-800 rounded-xl shadow-lg border border-gray-200 dark:border-gray-700 p-4 min-w-[280px]">
      {/* Header */}
      <div className="flex items-start justify-between gap-3 mb-3">
        <div className="flex items-center gap-2 flex-1 min-w-0">
          {isRunning && (
            <Loader2 className="h-4 w-4 text-brand-600 dark:text-brand-400 animate-spin flex-shrink-0" />
          )}
          {isDone && task.fail === 0 && (
            <CheckCircle2 className="h-4 w-4 text-success-600 dark:text-success-400 flex-shrink-0" />
          )}
          {(isError || (isDone && task.fail > 0)) && (
            <AlertCircle className={cn('h-4 w-4 flex-shrink-0', isError ? 'text-error-600 dark:text-error-400' : 'text-warning-500 dark:text-warning-400')} />
          )}
          <p className="text-sm font-medium text-gray-900 dark:text-white truncate">
            {task.label}
          </p>
        </div>
        {!isRunning && (
          <button
            onClick={onRemove}
            className="flex-shrink-0 p-0.5 rounded hover:bg-gray-100 dark:hover:bg-gray-700 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300 transition-colors"
            aria-label="Dismiss"
          >
            <X className="h-4 w-4" />
          </button>
        )}
      </div>

      {/* Progress bar */}
      <div className="h-1.5 bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden mb-2">
        <div
          className={cn(
            'h-full rounded-full transition-all duration-300',
            isError ? 'bg-error-500' : isDone && task.fail > 0 ? 'bg-warning-500' : 'bg-brand-600 dark:bg-brand-500'
          )}
          style={{ width: `${pct}%` }}
        />
      </div>

      {/* Stats */}
      <div className="flex items-center justify-between text-xs text-gray-500 dark:text-gray-400">
        <span>
          {task.done.toLocaleString()} / {task.total.toLocaleString()}
        </span>
        <span>
          {isRunning && `${pct}%`}
          {isDone && task.fail === 0 && (
            <span className="text-success-600 dark:text-success-400 font-medium">Completed</span>
          )}
          {isDone && task.fail > 0 && (
            <span className="text-warning-600 dark:text-warning-400 font-medium">
              {task.success.toLocaleString()} ok · {task.fail.toLocaleString()} failed
            </span>
          )}
          {isError && (
            <span className="text-error-600 dark:text-error-400 font-medium">Failed</span>
          )}
        </span>
      </div>
    </div>
  );
}

export function BackgroundTaskBar() {
  const { tasks, remove } = useBgTaskStore();

  if (tasks.length === 0) return null;

  return (
    <div className="fixed bottom-4 right-4 z-50 flex flex-col gap-2 pointer-events-none">
      {tasks.map((task) => (
        <div key={task.id} className="pointer-events-auto">
          <TaskCard task={task} onRemove={() => remove(task.id)} />
        </div>
      ))}
    </div>
  );
}
