import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { render } from '@/test/utils/test-utils';
import ClusterNodes from '@/pages/cluster/Nodes';
import APIClient from '@/lib/api';

vi.mock('@/lib/api', () => {
  const api = {
    getClusterConfig: vi.fn(),
    listClusterNodes: vi.fn(),
    addClusterNode: vi.fn(),
  };
  return { APIClient: api, default: api };
});

function node(id: string, isCoordinator: boolean) {
  return {
    id,
    name: id,
    endpoint: `https://${id}:8082`,
    priority: 100,
    health_status: 'healthy',
    latency_ms: 1,
    is_coordinator: isCoordinator,
  };
}

describe('Cluster nodes page', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(APIClient.getClusterConfig).mockResolvedValue({ node_id: 'n1', is_cluster_enabled: true } as never);
  });

  it('names the coordinator and the members', async () => {
    vi.mocked(APIClient.listClusterNodes).mockResolvedValue([node('n1', false), node('n2', true), node('n3', false)] as never);
    render(<ClusterNodes />);
    const coordinatorRow = (await screen.findByText('n2')).closest('tr')!;
    expect(within(coordinatorRow).getByText('Coordinator')).toBeInTheDocument();
    expect(screen.getAllByText('Member')).toHaveLength(2);
    expect(screen.queryByText('Electing a coordinator')).not.toBeInTheDocument();
  });

  it('says the nodes are electing a coordinator when none holds it', async () => {
    vi.mocked(APIClient.listClusterNodes).mockResolvedValue([node('n1', false), node('n2', false)] as never);
    render(<ClusterNodes />);
    expect(await screen.findByText('Electing a coordinator')).toBeInTheDocument();
    expect(screen.queryByText('Coordinator')).not.toBeInTheDocument();
  });

  it('adds a node with the join code opened on it, and no credentials of it', async () => {
    const user = userEvent.setup();
    vi.mocked(APIClient.listClusterNodes).mockResolvedValue([node('n1', true)] as never);
    vi.mocked(APIClient.addClusterNode).mockResolvedValue({ message: 'Node added to cluster successfully' });
    render(<ClusterNodes />);

    await user.click(await screen.findByRole('button', { name: 'Add Node' }));
    expect(screen.queryByPlaceholderText('admin')).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText('********')).not.toBeInTheDocument();
    await user.type(screen.getByPlaceholderText('192.168.1.10'), '10.0.1.20');
    await user.type(screen.getByPlaceholderText('XXXX-XXXX-XXXX-XXXX-XXXX-XXXX-XXXX-XXXX'), 'ABCD-EFGH-IJKL-MNOP-QRST-UVWX-YZ23-4567');
    await user.click(screen.getAllByRole('button', { name: 'Add Node' }).at(-1)!);

    expect(APIClient.addClusterNode).toHaveBeenCalledWith({
      endpoint: '10.0.1.20', join_code: 'ABCD-EFGH-IJKL-MNOP-QRST-UVWX-YZ23-4567', node_name: undefined,
    });
  });
});
