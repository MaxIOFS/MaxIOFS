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
} from 'recharts';
import { Card } from '@/components/ui/Card';

interface MetricLineChartProps {
  data: any[];
  title: string;
  dataKeys: { key: string; name: string; color: string }[];
  xAxisKey?: string;
  height?: number;
  formatYAxis?: (value: any) => string;
  formatTooltip?: (value: any) => string;
}

export const MetricLineChart: React.FC<MetricLineChartProps> = ({
  data,
  title,
  dataKeys,
  xAxisKey = 'timestamp',
  height = 300,
  formatYAxis,
  formatTooltip,
}) => {
  // Format timestamp for display
  const formatXAxis = (timestamp: number) => {
    const date = new Date(timestamp * 1000);
    return date.toLocaleTimeString('en-US', {
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  // Custom tooltip
  const CustomTooltip = ({ active, payload, label }: any) => {
    if (active && payload && payload.length) {
      const date = new Date(label * 1000);
      return (
        <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg p-3">
          <p className="text-sm font-medium text-gray-900 dark:text-white mb-2">
            {date.toLocaleString()}
          </p>
          {payload.map((entry: any, index: number) => (
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

  return (
    <Card>
      <div className="p-6">
        <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-4">{title}</h3>
        <ResponsiveContainer width="100%" height={height}>
          <LineChart data={data} margin={{ top: 5, right: 20, left: 10, bottom: 5 }}>
            <CartesianGrid strokeDasharray="3 3" className="stroke-gray-200 dark:stroke-gray-700" />
            <XAxis
              dataKey={xAxisKey}
              tickFormatter={formatXAxis}
              className="text-xs text-gray-600 dark:text-gray-400"
            />
            <YAxis
              tickFormatter={formatYAxis}
              className="text-xs text-gray-600 dark:text-gray-400"
              domain={[0, 'auto']}
              allowDataOverflow={false}
            />
            <Tooltip content={<CustomTooltip />} />
            <Legend wrapperStyle={{ fontSize: '12px' }} />
            {dataKeys.map((dk) => (
              <Line
                key={dk.key}
                type="monotone"
                dataKey={dk.key}
                name={dk.name}
                stroke={dk.color}
                strokeWidth={2}
                dot={false}
                activeDot={{ r: 4 }}
              />
            ))}
          </LineChart>
        </ResponsiveContainer>
      </div>
    </Card>
  );
};
