import { describe, it, expect, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { render } from '@/test/utils/test-utils';
import { ObjectDetailsView } from '@/components/ObjectDetailsView';

function view(objectData: Record<string, any>) {
  const noop = vi.fn();
  return render(
    <ObjectDetailsView
      bucketName="ssec"
      bucketPath="/buckets/ssec"
      currentPrefix=""
      objectKey="doc.txt"
      objectData={objectData}
      onBack={noop}
      onDownload={noop}
      onCopyUrl={noop}
      onCopyS3Uri={noop}
      onShare={noop}
      onPresignedUrl={noop}
      onRename={noop}
      onEditTags={noop}
      onDelete={noop}
    />,
  );
}

describe('ObjectDetailsView with an object encrypted with a customer key', () => {
  it('says the console cannot read it and turns off what would read it', async () => {
    view({ key: 'doc.txt', size: 10, etag: 'abc', customerKey: true });

    expect(screen.getByText(/Encrypted with a customer key \(SSE-C\)/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /download/i })).toBeDisabled();

    await userEvent.click(screen.getByRole('button', { name: /actions/i }));
    await waitFor(() => expect(screen.getByRole('button', { name: /presigned/i })).toBeDisabled());
    expect(screen.getByRole('button', { name: /rename/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /public link/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /tags/i })).toBeEnabled();
  });

  it('leaves another object as it was', () => {
    view({ key: 'doc.txt', size: 10, etag: 'abc' });

    expect(screen.queryByText(/Encrypted with a customer key/)).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /download/i })).toBeEnabled();
  });
});
