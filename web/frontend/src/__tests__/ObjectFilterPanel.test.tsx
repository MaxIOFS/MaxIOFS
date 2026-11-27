import { describe, it, expect, vi } from 'vitest';
import { fireEvent, screen } from '@testing-library/react';
import { render } from '@/test/utils/test-utils';
import { ObjectFilterPanel } from '@/components/ObjectFilterPanel';

// A size typed with decimals is sent as whole bytes, the only sizes the
// search takes.
describe('ObjectFilterPanel sizes', () => {
  it('gives whole bytes for a size typed with decimals', () => {
    const onFiltersChange = vi.fn();
    render(<ObjectFilterPanel filters={{}} onFiltersChange={onFiltersChange} onClear={vi.fn()} />);

    fireEvent.change(screen.getByPlaceholderText('Min'), { target: { value: '0.3' } });
    expect(onFiltersChange).toHaveBeenLastCalledWith({ minSize: 307 });

    fireEvent.change(screen.getByPlaceholderText('Max'), { target: { value: '1.1' } });
    expect(onFiltersChange).toHaveBeenLastCalledWith({ maxSize: 1153434 });
  });
});
