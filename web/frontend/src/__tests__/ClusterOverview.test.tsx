import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { render } from '@/test/utils/test-utils';
import ClusterOverview from '@/pages/cluster/Overview';
import APIClient from '@/lib/api';

vi.mock('@/lib/api', () => {
  const api = {
    getClusterConfig: vi.fn(),
    getClusterStatus: vi.fn(),
    openJoinCode: vi.fn(),
  };
  return { APIClient: api, default: api };
});

describe('Cluster page of a node in no cluster', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(APIClient.getClusterConfig).mockResolvedValue({ is_cluster_enabled: false } as never);
  });

  it('opens a join code to be added to a cluster', async () => {
    const user = userEvent.setup();
    vi.mocked(APIClient.openJoinCode).mockResolvedValue({
      join_code: 'ABCD-EFGH-IJKL-MNOP-QRST-UVWX-YZ23-4567', expires_at: Math.floor(Date.now() / 1000) + 900,
    });
    render(<ClusterOverview />);

    await user.click(await screen.findByRole('button', { name: 'Join code' }));

    expect(APIClient.openJoinCode).toHaveBeenCalledTimes(1);
    expect(await screen.findByText('ABCD-EFGH-IJKL-MNOP-QRST-UVWX-YZ23-4567')).toBeInTheDocument();
  });
});
