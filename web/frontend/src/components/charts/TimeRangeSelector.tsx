import React from 'react';
import { Clock } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { TIME_RANGES, type TimeRange } from './timeRanges';

interface TimeRangeSelectorProps {
  selected: TimeRange;
  onChange: (range: TimeRange) => void;
}

export const TimeRangeSelector: React.FC<TimeRangeSelectorProps> = ({
  selected,
  onChange,
}) => {
  const { t } = useTranslation('metrics');
  const realtimeLabel = t('realtimeOption');

  return (
    <div className="flex items-center space-x-2">
      <Clock className="h-4 w-4 text-muted-foreground" />
      <span className="text-sm font-medium text-foreground">{t('timeRangeLabel')}</span>
      <div className="inline-flex rounded-lg border border-border bg-card">
        {TIME_RANGES.map((range) => (
          <button
            key={range.label}
            onClick={() => onChange(range)}
            className={`px-3 py-1.5 text-sm font-medium transition-colors first:rounded-l-lg last:rounded-r-lg ${
              selected.label === range.label
                ? 'bg-brand-600 text-white'
                : 'text-foreground hover:bg-secondary'
            }`}
          >
            {range.label === 'Real-time' ? realtimeLabel : range.label}
          </button>
        ))}
      </div>
    </div>
  );
};
