import React from 'react';
import {
  BarChart,
  Bar,
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
  [key: string]: string | number;
}

interface MetricBarChartProps {
  data: DataPoint[];
  title: string;
  dataKeys: { key: string; name: string; color: string }[];
  xAxisKey?: string;
  height?: number;
  formatYAxis?: (value: number) => string;
  formatTooltip?: (value: number) => string;
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
    label?: string;
    formatTooltip?: (value: number) => string;
  };
  if (active && payload && payload.length) {
    return (
      <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg p-3">
        <p className="text-sm font-medium text-gray-900 dark:text-white mb-2">{label}</p>
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

export const MetricBarChart: React.FC<MetricBarChartProps> = ({
  data,
  title,
  dataKeys,
  xAxisKey = 'name',
  height = 300,
  formatYAxis,
  formatTooltip,
}) => {

  return (
    <Card>
      <div className="p-6">
        <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-4">{title}</h3>
        <ResponsiveContainer width="100%" height={height}>
          <BarChart data={data} margin={{ top: 5, right: 20, left: 10, bottom: 5 }}>
            <CartesianGrid strokeDasharray="3 3" className="stroke-gray-200 dark:stroke-gray-700" />
            <XAxis
              dataKey={xAxisKey}
              className="text-xs text-gray-600 dark:text-gray-400"
            />
            <YAxis
              tickFormatter={formatYAxis}
              className="text-xs text-gray-600 dark:text-gray-400"
            />
            <Tooltip content={<CustomTooltip formatTooltip={formatTooltip} />} />
            <Legend wrapperStyle={{ fontSize: '12px' }} />
            {dataKeys.map((dk) => (
              <Bar key={dk.key} dataKey={dk.key} name={dk.name} fill={dk.color} />
            ))}
          </BarChart>
        </ResponsiveContainer>
      </div>
    </Card>
  );
};
