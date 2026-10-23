import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, within } from '@testing-library/react';
import { render } from '@/test/utils/test-utils';
import ClusterNodes from '@/pages/cluster/Nodes';
import APIClient from '@/lib/api';

vi.mock('@/lib/api', () => {
  const api = {
    getClusterConfig: vi.fn(),
    listClusterNodes: vi.fn(),
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
});
