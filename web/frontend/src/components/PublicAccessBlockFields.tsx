import { useTranslation } from 'react-i18next';
import type { PublicAccessBlockConfig } from '@/types';

const PUBLIC_ACCESS_FLAGS = ['blockPublicAcls', 'ignorePublicAcls', 'blockPublicPolicy', 'restrictPublicBuckets'] as const;

interface PublicAccessBlockFieldsProps {
  value: PublicAccessBlockConfig;
  onChange: (value: PublicAccessBlockConfig) => void;
  disabled?: boolean;
}

// The four Block Public Access flags of a bucket, each with what it does.
export function PublicAccessBlockFields({ value, onChange, disabled }: PublicAccessBlockFieldsProps) {
  const { t } = useTranslation('bucketSettings');
  return (
    <div className="space-y-3">
      {PUBLIC_ACCESS_FLAGS.map((flag) => (
        <label key={flag} className="flex items-start gap-3">
          <input
            type="checkbox"
            checked={value[flag]}
            onChange={(e) => onChange({ ...value, [flag]: e.target.checked })}
            disabled={disabled}
            className="mt-1 rounded border-border"
          />
          <div>
            <div className="text-sm font-medium text-foreground">{t(`publicAccess.${flag}`)}</div>
            <div className="text-xs text-muted-foreground">{t(`publicAccess.${flag}Help`)}</div>
          </div>
        </label>
      ))}
    </div>
  );
}
