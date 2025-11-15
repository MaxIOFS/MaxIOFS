import React from 'react';
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  Legend,
  ResponsiveContainer,
  TooltipProps,
} from 'recharts';
import { Card } from '@/components/ui/Card';

interface DataPoint {
  [key: string]: string | number | null;
}

interface MetricLineChartProps {
  data: DataPoint[];
  title: string;
  dataKeys: { key: string; name: string; color: string }[];
  xAxisKey?: string;
  height?: number;
  formatYAxis?: (value: number) => string;
  formatTooltip?: (value: number) => string;
  timeRange?: { start: number; end: number }; // Unix timestamps in seconds
}

interface TooltipPayload {
  name: string;
  value: number;
  color: string;
}

// Custom tooltip component moved outside to prevent re-creation on each render
const CustomTooltip: React.FC<
  TooltipProps<number, string> & { formatTooltip?: (value: number) => string }
> = (props) => {
  const { active, payload, label, formatTooltip } = props as {
    active?: boolean;
    payload?: TooltipPayload[];
    label?: number;
    formatTooltip?: (value: number) => string;
  };
  if (active && payload && payload.length) {
    const date = new Date((label as number) * 1000);
    return (
      <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg p-3">
        <p className="text-sm font-medium text-gray-900 dark:text-white mb-2">
          {date.toLocaleString()}
        </p>
        {payload.map((entry: TooltipPayload, index: number) => (
          <p key={index} className="text-sm" style={{ color: entry.color }}>
            {entry.name}:{' '}
            {formatTooltip ? formatTooltip(entry.value) : entry.value.toFixed(2)}
          </p>
        ))}
      </div>
    );
  }
  return null;
};

export const MetricLineChart: React.FC<MetricLineChartProps> = React.memo(({
  data,
  title,
  dataKeys,
  xAxisKey = 'timestamp',
  height = 300,
  formatYAxis,
  formatTooltip,
  timeRange,
}) => {
  // Adaptive downsampling based on data size for optimal performance
  const processedData = React.useMemo(() => {
    // Calculate max points based on time range
    const calculateMaxPoints = () => {
      if (!timeRange) return 500;
      const hours = (timeRange.end - timeRange.start) / 3600;
      
      if (hours <= 1) return 500;         // 1h: 500 points
      if (hours <= 6) return 400;         // 6h: 400 points
      if (hours <= 24) return 300;        // 24h: 300 points
      if (hours <= 168) return 250;       // 7d: 250 points
      if (hours <= 720) return 200;       // 30d: 200 points
      return 150;                         // 1y: 150 points (aggressive downsampling)
    };
    
    const maxPoints = calculateMaxPoints();
    
    if (data.length <= maxPoints) return data;
    
    // Downsample by taking every nth point
    const step = Math.ceil(data.length / maxPoints);
    return data.filter((_, index) => index % step === 0);
  }, [data, timeRange]);

  // Add boundary markers to ensure full time range is displayed
  const dataWithBoundaries = React.useMemo(() => {
    if (!timeRange) return processedData;

    const { start, end } = timeRange;

    // Create boundary markers with null values (invisible but fix axis domain)
    const startMarker: DataPoint = { [xAxisKey]: start };
    const endMarker: DataPoint = { [xAxisKey]: end };

    dataKeys.forEach(dk => {
      startMarker[dk.key] = null;
      endMarker[dk.key] = null;
    });

    // Add boundaries only if data doesn't already cover them
    const result = [...processedData];
    const firstTimestamp = processedData.length > 0 ? (processedData[0][xAxisKey] as number) : Number.MAX_VALUE;
    const lastTimestamp = processedData.length > 0 ? (processedData[processedData.length - 1][xAxisKey] as number) : 0;

    if (start < firstTimestamp) {
      result.unshift(startMarker);
    }
    if (end > lastTimestamp) {
      result.push(endMarker);
    }

    return result;
  }, [processedData, timeRange, xAxisKey, dataKeys]);

  // Format timestamp for display
  const formatXAxis = (timestamp: number) => {
    const date = new Date(timestamp * 1000);
    return date.toLocaleTimeString('en-US', {
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  // Determine X axis domain (fixed to requested time range)
  const xAxisDomain = React.useMemo(() => {
    if (timeRange) {
      return [timeRange.start, timeRange.end];
    }
    return ['auto', 'auto'];
  }, [timeRange]);

  return (
    <Card>
      <div className="p-6">
        <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-4">{title}</h3>
        <ResponsiveContainer width="100%" height={height}>
          <LineChart data={dataWithBoundaries} margin={{ top: 5, right: 20, left: 10, bottom: 5 }}>
            <CartesianGrid strokeDasharray="3 3" className="stroke-gray-200 dark:stroke-gray-700" />
            <XAxis
              dataKey={xAxisKey}
              tickFormatter={formatXAxis}
              className="text-xs text-gray-600 dark:text-gray-400"
              domain={xAxisDomain}
              type="number"
              scale="time"
            />
            <YAxis
              tickFormatter={formatYAxis}
              className="text-xs text-gray-600 dark:text-gray-400"
              domain={[0, 'auto']}
              allowDataOverflow={false}
            />
            <Tooltip content={<CustomTooltip formatTooltip={formatTooltip} />} />
            <Legend wrapperStyle={{ fontSize: '12px' }} />
            {dataKeys.map((dk) => (
              <Line
                key={dk.key}
                type="linear"
                dataKey={dk.key}
                name={dk.name}
                stroke={dk.color}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4 }}
                connectNulls={false}
              />
            ))}
          </LineChart>
        </ResponsiveContainer>
      </div>
    </Card>
  );
});
