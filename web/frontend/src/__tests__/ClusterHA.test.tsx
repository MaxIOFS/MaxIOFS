import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { render } from '@/test/utils/test-utils';
import ClusterHA from '@/pages/cluster/HA';
import APIClient from '@/lib/api';
import ModalManager from '@/lib/modals';

vi.mock('@/lib/api', () => {
  const api = {
    getClusterConfig: vi.fn(),
    getClusterHA: vi.fn(),
    getClusterHASyncJobs: vi.fn(),
    getClusterDegradedState: vi.fn(),
    getHAScrubStatus: vi.fn(),
    setClusterHA: vi.fn(),
    drainClusterNode: vi.fn(),
  };
  return { APIClient: api, default: api };
});

vi.mock('@/lib/modals', () => ({
  default: {
    confirm: vi.fn((_title: string, _text: string, onConfirm?: () => void) => onConfirm?.()),
    toast: vi.fn(),
    apiError: vi.fn(),
  },
}));

function node(id: string) {
  return {
    id,
    name: id,
    health_status: 'healthy',
    capacity_total: 1000,
    capacity_used: 100,
    capacity_free: 900,
    drained: false,
    storage_class: 'STANDARD',
  };
}

function haState(over: Partial<Awaited<ReturnType<typeof APIClient.getClusterHA>>>) {
  return {
    replication_factor: 2,
    parity: false,
    data_shards: 0,
    parity_shards: 0,
    node_count: 3,
    tolerated_failures: 1,
    total_bytes: 3000,
    usable_bytes: 1500,
    local_node_id: 'a',
    nodes: [node('a'), node('b'), node('c')],
    ...over,
  };
}

describe('Cluster HA page', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(APIClient.getClusterConfig).mockResolvedValue({ is_cluster_enabled: true } as never);
    vi.mocked(APIClient.getClusterHASyncJobs).mockResolvedValue({ sync_jobs: [] } as never);
    vi.mocked(APIClient.getClusterDegradedState).mockResolvedValue({ degraded: false, reason: '' } as never);
    vi.mocked(APIClient.getHAScrubStatus).mockResolvedValue({ runs: [], current: null } as never);
  });

  it('shows the layout of a write with parity', async () => {
    vi.mocked(APIClient.getClusterHA).mockResolvedValue(
      haState({ parity: true, data_shards: 2, parity_shards: 1, usable_bytes: 2000 }),
    );
    render(<ClusterHA />);
    expect(await screen.findByText('2+1')).toBeInTheDocument();
    expect(screen.getByText('2 data + 1 parity shards')).toBeInTheDocument();
  });

  it('turns parity on', async () => {
    vi.mocked(APIClient.getClusterHA).mockResolvedValue(haState({}));
    vi.mocked(APIClient.setClusterHA).mockResolvedValue({
      message: 'Parity turned on', previous_factor: 2, previous_parity: false, new_factor: 2, parity: true,
    });
    const user = userEvent.setup();
    render(<ClusterHA />);
    await user.click(await screen.findByRole('button', { name: /Each object is cut in data shards/ }));
    await user.click(screen.getByRole('button', { name: 'Apply parity' }));
    await waitFor(() => expect(APIClient.setClusterHA).toHaveBeenCalledWith('parity'));
    expect(ModalManager.confirm).toHaveBeenCalledWith(
      'Change data protection?',
      expect.stringContaining('You are turning parity on.'),
      expect.any(Function),
    );
    await waitFor(() => expect(ModalManager.toast).toHaveBeenCalledWith('success', 'Parity turned on'));
  });

  it('turns parity off for whole copies', async () => {
    vi.mocked(APIClient.getClusterHA).mockResolvedValue(haState({ parity: true, data_shards: 2, parity_shards: 1 }));
    vi.mocked(APIClient.setClusterHA).mockResolvedValue({
      message: 'Replication factor updated', previous_factor: 2, previous_parity: true, new_factor: 3, parity: false,
    });
    const user = userEvent.setup();
    render(<ClusterHA />);
    await user.click(await screen.findByRole('button', { name: /Three copies of each object/ }));
    await user.click(screen.getByRole('button', { name: 'Apply ×3' }));
    await waitFor(() => expect(APIClient.setClusterHA).toHaveBeenCalledWith(3));
    expect(ModalManager.confirm).toHaveBeenCalledWith(
      'Change data protection?',
      expect.stringContaining('You are turning parity off for ×3 whole copies.'),
      expect.any(Function),
    );
  });

  it('measures the protection of STANDARD by its nodes, with other classes kept', async () => {
    vi.mocked(APIClient.getClusterHA).mockResolvedValue(haState({
      node_count: 5,
      standard_nodes: 2,
      storage_classes: 2,
      nodes: [node('a'), node('b'), { ...node('c'), storage_class: 'GLACIER' }],
    }));
    render(<ClusterHA />);
    const parity = await screen.findByRole('button', { name: /Each object is cut in data shards/ });
    expect(parity).toBeDisabled();
    expect(parity).toHaveTextContent('Requires 3 healthy nodes — cluster has 2');
    expect(screen.getByText('This is the protection of STANDARD. Each other storage class has its own.')).toBeInTheDocument();
    expect(screen.getByText('GLACIER')).toBeInTheDocument();
    expect(screen.getAllByText('STANDARD')).toHaveLength(2);
  });

  it('needs three nodes for parity', async () => {
    vi.mocked(APIClient.getClusterHA).mockResolvedValue(haState({ node_count: 2, nodes: [node('a'), node('b')] }));
    render(<ClusterHA />);
    const parity = await screen.findByRole('button', { name: /Each object is cut in data shards/ });
    expect(parity).toBeDisabled();
    expect(parity).toHaveTextContent('Requires 3 healthy nodes — cluster has 2');
  });
});
