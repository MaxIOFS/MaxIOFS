import React from 'react';
import { Clock } from 'lucide-react';

export interface TimeRange {
  label: string;
  hours: number;
}

interface TimeRangeSelectorProps {
  selected: TimeRange;
  onChange: (range: TimeRange) => void;
}

export const TIME_RANGES: TimeRange[] = [
  { label: '1H', hours: 1 },
  { label: '6H', hours: 6 },
  { label: '24H', hours: 24 },
  { label: '7D', hours: 24 * 7 },
  { label: '30D', hours: 24 * 30 },
  { label: '1Y', hours: 24 * 365 },
];

export const TimeRangeSelector: React.FC<TimeRangeSelectorProps> = ({
  selected,
  onChange,
}) => {
  return (
    <div className="flex items-center space-x-2">
      <Clock className="h-4 w-4 text-gray-500 dark:text-gray-400" />
      <span className="text-sm font-medium text-gray-700 dark:text-gray-300">Time Range:</span>
      <div className="inline-flex rounded-lg border border-gray-200 dark:border-gray-700 bg-white dark:bg-gray-800">
        {TIME_RANGES.map((range) => (
          <button
            key={range.label}
            onClick={() => onChange(range)}
            className={`px-3 py-1.5 text-sm font-medium transition-colors first:rounded-l-lg last:rounded-r-lg ${
              selected.label === range.label
                ? 'bg-brand-600 text-white'
                : 'text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700'
            }`}
          >
            {range.label}
          </button>
        ))}
      </div>
    </div>
  );
};
