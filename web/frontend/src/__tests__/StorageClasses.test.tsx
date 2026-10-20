import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { render } from '@/test/utils/test-utils';
import StorageClasses from '@/pages/cluster/StorageClasses';
import APIClient from '@/lib/api';
import ModalManager from '@/lib/modals';
import type { StorageClassState, StorageClassesResponse } from '@/types';

vi.mock('@/lib/api', () => {
  const api = {
    getClusterConfig: vi.fn(),
    getStorageClasses: vi.fn(),
    setStorageClass: vi.fn(),
    deleteStorageClass: vi.fn(),
    setNodeStorageClass: vi.fn(),
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

function storageClass(over: Partial<StorageClassState>): StorageClassState {
  return {
    name: 'STANDARD', factor: 2, parity: false, data_shards: 0, parity_shards: 0, node_count: 2, takers: 2,
    required_nodes: 2, total_bytes: 2000, usable_bytes: 1000, tolerated_failures: 1, below_protection: false,
    ...over,
  };
}

function node(id: string, storage_class: string) {
  return { id, name: id, storage_class, health_status: 'healthy', drained: false, capacity_total: 1000, capacity_used: 100 };
}

function state(over: Partial<StorageClassesResponse> = {}): StorageClassesResponse {
  return {
    classes: [
      storageClass({}),
      storageClass({ name: 'GLACIER', parity: true, factor: 1, data_shards: 2, parity_shards: 1, node_count: 3, takers: 3, required_nodes: 3 }),
    ],
    nodes: [node('a', 'STANDARD'), node('b', 'STANDARD'), node('c', 'GLACIER'), node('d', 'GLACIER'), node('e', 'GLACIER')],
    definable: ['STANDARD_IA', 'DEEP_ARCHIVE'],
    ...over,
  };
}

describe('Storage classes page', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(APIClient.getClusterConfig).mockResolvedValue({ is_cluster_enabled: true } as never);
    vi.mocked(APIClient.setStorageClass).mockResolvedValue({ message: 'Storage class set', name: 'x' });
    vi.mocked(APIClient.deleteStorageClass).mockResolvedValue({ message: 'Storage class deleted', name: 'x' });
    vi.mocked(APIClient.setNodeStorageClass).mockResolvedValue({ message: 'Node storage class set', node_id: 'b', storage_class: 'GLACIER' });
  });

  it('shows each class with how it keeps its data', async () => {
    vi.mocked(APIClient.getStorageClasses).mockResolvedValue(state());
    render(<StorageClasses />);
    expect(await screen.findByText('×2')).toBeInTheDocument();
    expect(screen.getByText('2+1')).toBeInTheDocument();
    expect(screen.getByText('3 nodes')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Set on the HA page/ })).toBeInTheDocument();
    expect(screen.getAllByRole('button', { name: /Delete/ })).toHaveLength(1);
  });

  it('warns of a class with fewer nodes than its protection needs', async () => {
    vi.mocked(APIClient.getStorageClasses).mockResolvedValue(state({
      classes: [
        storageClass({}),
        storageClass({ name: 'GLACIER', node_count: 1, takers: 1, below_protection: true }),
        storageClass({ name: 'DEEP_ARCHIVE', factor: 1, node_count: 0, takers: 0, required_nodes: 1, below_protection: true }),
      ],
    }));
    render(<StorageClasses />);
    expect(await screen.findByText(/Nodes taking data: 1 of the 2 its protection needs/)).toBeInTheDocument();
    expect(screen.getByText(/No node keeps this class yet/)).toBeInTheDocument();
  });

  it('adds a class with its protection', async () => {
    vi.mocked(APIClient.getStorageClasses).mockResolvedValue(state());
    const user = userEvent.setup();
    render(<StorageClasses />);
    const add = await screen.findByRole('button', { name: 'Add' });
    expect(add).toBeDisabled();
    await user.selectOptions(screen.getByLabelText('Class'), 'DEEP_ARCHIVE');
    await user.selectOptions(screen.getByLabelText('Protection of the new class'), 'parity');
    await user.click(add);
    await waitFor(() => expect(APIClient.setStorageClass).toHaveBeenCalledWith('DEEP_ARCHIVE', 'parity'));
    await waitFor(() => expect(ModalManager.toast).toHaveBeenCalledWith('success', 'Storage class DEEP_ARCHIVE saved'));
  });

  it('changes the protection of a class and deletes it', async () => {
    vi.mocked(APIClient.getStorageClasses).mockResolvedValue(state());
    const user = userEvent.setup();
    render(<StorageClasses />);
    await user.selectOptions(await screen.findByLabelText('Protection of GLACIER'), '3');
    await waitFor(() => expect(APIClient.setStorageClass).toHaveBeenCalledWith('GLACIER', 3));
    expect(ModalManager.confirm).toHaveBeenCalledWith('Change the protection of GLACIER?', expect.stringContaining('Three copies'), expect.any(Function));

    await user.click(screen.getByRole('button', { name: /Delete/ }));
    await waitFor(() => expect(APIClient.deleteStorageClass).toHaveBeenCalledWith('GLACIER'));
    expect(ModalManager.confirm).toHaveBeenCalledWith('Delete storage class', expect.stringContaining('go back to STANDARD'), expect.any(Function));
  });

  it('moves a node to another class', async () => {
    vi.mocked(APIClient.getStorageClasses).mockResolvedValue(state());
    const user = userEvent.setup();
    render(<StorageClasses />);
    const select = await screen.findByLabelText('Storage class of b');
    expect(within(select).getAllByRole('option').map((o) => o.textContent)).toEqual(['STANDARD', 'GLACIER']);
    await user.selectOptions(select, 'GLACIER');
    await waitFor(() => expect(APIClient.setNodeStorageClass).toHaveBeenCalledWith('b', 'GLACIER'));
    expect(ModalManager.confirm).toHaveBeenCalledWith('Move node to another class', expect.stringContaining('Move b from STANDARD to GLACIER?'), expect.any(Function));
    await waitFor(() => expect(ModalManager.toast).toHaveBeenCalledWith('success', 'b is now in GLACIER'));
  });

  it('says when the cluster keeps every class', async () => {
    vi.mocked(APIClient.getStorageClasses).mockResolvedValue(state({ definable: [] }));
    render(<StorageClasses />);
    expect(await screen.findByText('The cluster keeps every storage class it can.')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Add' })).not.toBeInTheDocument();
  });
});
