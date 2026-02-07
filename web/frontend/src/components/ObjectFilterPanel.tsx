import React, { useState } from 'react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { X as XIcon } from 'lucide-react';
import { Plus as PlusIcon } from 'lucide-react';
import { Filter as FilterIcon } from 'lucide-react';
import type { ObjectSearchFilter } from '@/types';

interface ObjectFilterPanelProps {
  filters: ObjectSearchFilter;
  onFiltersChange: (filters: ObjectSearchFilter) => void;
  onClear: () => void;
}

const CONTENT_TYPE_PRESETS = [
  { label: 'Images', prefix: 'image/' },
  { label: 'Documents', prefix: 'application/pdf' },
  { label: 'Videos', prefix: 'video/' },
  { label: 'Archives', prefix: 'application/zip' },
  { label: 'Text', prefix: 'text/' },
];

type SizeUnit = 'B' | 'KB' | 'MB' | 'GB';

function toBytes(value: number, unit: SizeUnit): number {
  switch (unit) {
    case 'KB': return value * 1024;
    case 'MB': return value * 1024 * 1024;
    case 'GB': return value * 1024 * 1024 * 1024;
    default: return value;
  }
}

function fromBytes(bytes: number): { value: number; unit: SizeUnit } {
  if (bytes >= 1024 * 1024 * 1024) return { value: Math.round(bytes / (1024 * 1024 * 1024)), unit: 'GB' };
  if (bytes >= 1024 * 1024) return { value: Math.round(bytes / (1024 * 1024)), unit: 'MB' };
  if (bytes >= 1024) return { value: Math.round(bytes / 1024), unit: 'KB' };
  return { value: bytes, unit: 'B' };
}

export function ObjectFilterPanel({ filters, onFiltersChange, onClear }: ObjectFilterPanelProps) {
  const [minSizeUnit, setMinSizeUnit] = useState<SizeUnit>('KB');
  const [maxSizeUnit, setMaxSizeUnit] = useState<SizeUnit>('MB');
  const [newTagKey, setNewTagKey] = useState('');
  const [newTagValue, setNewTagValue] = useState('');

  const minSizeDisplay = filters.minSize !== undefined ? fromBytes(filters.minSize) : null;
  const maxSizeDisplay = filters.maxSize !== undefined ? fromBytes(filters.maxSize) : null;

  const toggleContentType = (prefix: string) => {
    const current = filters.contentTypes || [];
    const updated = current.includes(prefix)
      ? current.filter(ct => ct !== prefix)
      : [...current, prefix];
    onFiltersChange({ ...filters, contentTypes: updated.length > 0 ? updated : undefined });
  };

  const handleMinSizeChange = (value: string) => {
    if (value === '') {
      onFiltersChange({ ...filters, minSize: undefined });
      return;
    }
    const num = parseFloat(value);
    if (!isNaN(num) && num >= 0) {
      onFiltersChange({ ...filters, minSize: toBytes(num, minSizeUnit) });
    }
  };

  const handleMaxSizeChange = (value: string) => {
    if (value === '') {
      onFiltersChange({ ...filters, maxSize: undefined });
      return;
    }
    const num = parseFloat(value);
    if (!isNaN(num) && num >= 0) {
      onFiltersChange({ ...filters, maxSize: toBytes(num, maxSizeUnit) });
    }
  };

  const addTag = () => {
    if (newTagKey.trim() && newTagValue.trim()) {
      const tags = { ...(filters.tags || {}), [newTagKey.trim()]: newTagValue.trim() };
      onFiltersChange({ ...filters, tags });
      setNewTagKey('');
      setNewTagValue('');
    }
  };

  const removeTag = (key: string) => {
    const tags = { ...(filters.tags || {}) };
    delete tags[key];
    onFiltersChange({ ...filters, tags: Object.keys(tags).length > 0 ? tags : undefined });
  };

  const activeFilterCount = [
    filters.contentTypes && filters.contentTypes.length > 0,
    filters.minSize !== undefined,
    filters.maxSize !== undefined,
    filters.modifiedAfter,
    filters.modifiedBefore,
    filters.tags && Object.keys(filters.tags).length > 0,
  ].filter(Boolean).length;

  return (
    <div className="bg-white dark:bg-gray-800 rounded-xl border border-gray-200 dark:border-gray-700 shadow-sm p-4 space-y-4">
      <div className="flex items-center justify-between">
        <h4 className="text-sm font-semibold text-gray-900 dark:text-white flex items-center gap-2">
          <FilterIcon className="h-4 w-4" />
          Advanced Filters
          {activeFilterCount > 0 && (
            <span className="bg-brand-100 dark:bg-brand-900 text-brand-700 dark:text-brand-300 text-xs px-2 py-0.5 rounded-full">
              {activeFilterCount}
            </span>
          )}
        </h4>
        {activeFilterCount > 0 && (
          <Button onClick={onClear} variant="ghost" size="sm" className="text-xs">
            Clear all
          </Button>
        )}
      </div>

      {/* Content Type */}
      <div>
        <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">Content Type</label>
        <div className="flex flex-wrap gap-2">
          {CONTENT_TYPE_PRESETS.map(({ label, prefix }) => (
            <button
              key={prefix}
              onClick={() => toggleContentType(prefix)}
              className={`px-3 py-1.5 text-xs rounded-lg border transition-colors ${
                (filters.contentTypes || []).includes(prefix)
                  ? 'bg-brand-100 dark:bg-brand-900 border-brand-300 dark:border-brand-700 text-brand-700 dark:text-brand-300'
                  : 'bg-gray-50 dark:bg-gray-700 border-gray-200 dark:border-gray-600 text-gray-600 dark:text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-600'
              }`}
            >
              {label}
            </button>
          ))}
        </div>
      </div>

      {/* Size Range */}
      <div>
        <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">Size Range</label>
        <div className="flex items-center gap-2">
          <div className="flex items-center gap-1">
            <Input
              type="number"
              placeholder="Min"
              value={minSizeDisplay ? minSizeDisplay.value : ''}
              onChange={(e) => handleMinSizeChange(e.target.value)}
              className="w-24 text-sm"
              min={0}
            />
            <select
              value={minSizeUnit}
              onChange={(e) => setMinSizeUnit(e.target.value as SizeUnit)}
              className="text-xs border border-gray-300 dark:border-gray-600 rounded-md px-2 py-2 bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-300"
            >
              <option value="B">B</option>
              <option value="KB">KB</option>
              <option value="MB">MB</option>
              <option value="GB">GB</option>
            </select>
          </div>
          <span className="text-gray-400 text-xs">to</span>
          <div className="flex items-center gap-1">
            <Input
              type="number"
              placeholder="Max"
              value={maxSizeDisplay ? maxSizeDisplay.value : ''}
              onChange={(e) => handleMaxSizeChange(e.target.value)}
              className="w-24 text-sm"
              min={0}
            />
            <select
              value={maxSizeUnit}
              onChange={(e) => setMaxSizeUnit(e.target.value as SizeUnit)}
              className="text-xs border border-gray-300 dark:border-gray-600 rounded-md px-2 py-2 bg-white dark:bg-gray-700 text-gray-700 dark:text-gray-300"
            >
              <option value="B">B</option>
              <option value="KB">KB</option>
              <option value="MB">MB</option>
              <option value="GB">GB</option>
            </select>
          </div>
        </div>
      </div>

      {/* Date Range */}
      <div>
        <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">Date Range</label>
        <div className="flex items-center gap-2">
          <Input
            type="date"
            value={filters.modifiedAfter ? filters.modifiedAfter.split('T')[0] : ''}
            onChange={(e) => {
              const val = e.target.value;
              onFiltersChange({
                ...filters,
                modifiedAfter: val ? new Date(val).toISOString() : undefined,
              });
            }}
            className="text-sm"
          />
          <span className="text-gray-400 text-xs">to</span>
          <Input
            type="date"
            value={filters.modifiedBefore ? filters.modifiedBefore.split('T')[0] : ''}
            onChange={(e) => {
              const val = e.target.value;
              onFiltersChange({
                ...filters,
                modifiedBefore: val ? new Date(val + 'T23:59:59Z').toISOString() : undefined,
              });
            }}
            className="text-sm"
          />
        </div>
      </div>

      {/* Tags */}
      <div>
        <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-2">Tags</label>
        {filters.tags && Object.keys(filters.tags).length > 0 && (
          <div className="flex flex-wrap gap-2 mb-2">
            {Object.entries(filters.tags).map(([key, value]) => (
              <span
                key={key}
                className="inline-flex items-center gap-1 px-2 py-1 text-xs bg-gray-100 dark:bg-gray-700 text-gray-700 dark:text-gray-300 rounded-md"
              >
                {key}={value}
                <button onClick={() => removeTag(key)} className="text-gray-400 hover:text-gray-600">
                  <XIcon className="h-3 w-3" />
                </button>
              </span>
            ))}
          </div>
        )}
        <div className="flex items-center gap-2">
          <Input
            placeholder="Key"
            value={newTagKey}
            onChange={(e) => setNewTagKey(e.target.value)}
            className="w-28 text-sm"
            onKeyDown={(e) => e.key === 'Enter' && addTag()}
          />
          <Input
            placeholder="Value"
            value={newTagValue}
            onChange={(e) => setNewTagValue(e.target.value)}
            className="w-28 text-sm"
            onKeyDown={(e) => e.key === 'Enter' && addTag()}
          />
          <Button onClick={addTag} variant="outline" size="sm" className="gap-1">
            <PlusIcon className="h-3 w-3" />
            Add
          </Button>
        </div>
      </div>
    </div>
  );
}
