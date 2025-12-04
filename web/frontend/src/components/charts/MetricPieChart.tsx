import React, { useState, useEffect } from 'react';
import {
  PieChart,
  Pie,
  Cell,
  Tooltip,
  Legend,
  ResponsiveContainer,
  TooltipProps,
  PieLabelRenderProps,
} from 'recharts';
import { Card } from '@/components/ui/Card';

interface PieDataPoint {
  name: string;
  value: number;
  [key: string]: string | number;
}

interface MetricPieChartProps {
  data: PieDataPoint[];
  title: string;
  colors?: string[];
  height?: number;
  formatTooltip?: (value: number) => string;
}

interface TooltipPayload {
  name: string;
  value: number;
  payload: {
    fill: string;
  };
}

const DEFAULT_COLORS = [
  '#3b82f6', // blue
  '#10b981', // green
  '#f59e0b', // yellow
  '#ef4444', // red
  '#8b5cf6', // purple
  '#ec4899', // pink
  '#06b6d4', // cyan
  '#f97316', // orange
];

// Custom tooltip component moved outside to prevent re-creation on each render
const CustomTooltip: React.FC<
  TooltipProps<number, string> & {
    formatTooltip?: (value: number) => string;
    data: PieDataPoint[];
  }
> = (props) => {
  const { active, payload, formatTooltip, data } = props as {
    active?: boolean;
    payload?: TooltipPayload[];
    formatTooltip?: (value: number) => string;
    data: PieDataPoint[];
  };
  if (active && payload && payload.length) {
    const entry = payload[0] as unknown as TooltipPayload;
    const totalValue = data.reduce((acc, item) => acc + item.value, 0);
    const percentage = ((entry.value / totalValue) * 100).toFixed(1);

    return (
      <div className="bg-white dark:bg-gray-800 border border-gray-200 dark:border-gray-700 rounded-lg shadow-lg p-3">
        <p className="text-sm font-medium text-gray-900 dark:text-white">{entry.name}</p>
        <p className="text-sm" style={{ color: entry.payload.fill }}>
          Value: {formatTooltip ? formatTooltip(entry.value) : entry.value.toFixed(2)}
        </p>
        <p className="text-sm text-gray-600 dark:text-gray-400">
          {percentage}%
        </p>
      </div>
    );
  }
  return null;
};

export const MetricPieChart: React.FC<MetricPieChartProps> = ({
  data,
  title,
  colors = DEFAULT_COLORS,
  height = 300,
  formatTooltip,
}) => {
  // Calculate responsive outer radius based on window width
  const getOuterRadius = () => {
    const width = window.innerWidth;
    if (width >= 3840) return 180; // 4K
    if (width >= 2560) return 140; // 2K
    if (width >= 1920) return 120; // Full HD+
    return 100; // Default
  };

  const [outerRadius, setOuterRadius] = useState(getOuterRadius());

  useEffect(() => {
    const handleResize = () => {
      setOuterRadius(getOuterRadius());
    };

    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  // Custom label using useCallback to prevent re-creation
  const renderLabel = React.useCallback((props: PieLabelRenderProps) => {
    const value = props.value as number | undefined;
    if (!value) return '';
    const totalValue = data.reduce((acc, item) => acc + item.value, 0);
    const percent = ((value / totalValue) * 100).toFixed(0);
    return `${percent}%`;
  }, [data]);

  return (
    <Card>
      <div className="p-6">
        <h3 className="text-lg font-semibold text-gray-900 dark:text-white mb-4">{title}</h3>
        <ResponsiveContainer width="100%" height={height}>
          <PieChart>
            <Pie
              data={data}
              cx="50%"
              cy="50%"
              labelLine={false}
              label={renderLabel}
              outerRadius={outerRadius}
              fill="#8884d8"
              dataKey="value"
            >
              {data.map((entry, index) => (
                <Cell key={`cell-${index}`} fill={colors[index % colors.length]} />
              ))}
            </Pie>
            <Tooltip content={<CustomTooltip formatTooltip={formatTooltip} data={data} />} />
            <Legend wrapperStyle={{ fontSize: '12px' }} />
          </PieChart>
        </ResponsiveContainer>
      </div>
    </Card>
  );
};
