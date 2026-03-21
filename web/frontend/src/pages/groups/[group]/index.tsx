import React, { useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Modal } from '@/components/ui/Modal';
import { Loading } from '@/components/ui/Loading';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/Table';
import { Users, ArrowLeft, Plus, Trash2, Save, UserMinus } from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { GroupMember, User } from '@/types';
import ModalManager from '@/lib/modals';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { formatRelativeTime } from '@/lib/utils';

export default function GroupDetailPage() {
  const { group: groupId } = useParams<{ group: string }>();
  const navigate = useNavigate();
  const { isGlobalAdmin, isTenantAdmin } = useCurrentUser();
  const queryClient = useQueryClient();

  const [isAddMemberOpen, setIsAddMemberOpen] = useState(false);
  const [selectedUserId, setSelectedUserId] = useState('');
  const [isEditing, setIsEditing] = useState(false);
  const [editForm, setEditForm] = useState({ displayName: '', description: '' });

  const isAnyAdmin = isGlobalAdmin || isTenantAdmin;

  const { data: group, isLoading: groupLoading } = useQuery({
    queryKey: ['groups', groupId],
    queryFn: () => APIClient.getGroup(groupId!),
    enabled: !!groupId,
  });

  const { data: members = [], isLoading: membersLoading } = useQuery({
    queryKey: ['groups', groupId, 'members'],
    queryFn: () => APIClient.listGroupMembers(groupId!),
    enabled: !!groupId,
  });

  const { data: allUsers = [] } = useQuery({
    queryKey: ['users'],
    queryFn: () => APIClient.listUsers(),
    enabled: isAddMemberOpen,
  });

  const updateMutation = useMutation({
    mutationFn: (data: { displayName: string; description: string }) =>
      APIClient.updateGroup(groupId!, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['groups', groupId] });
      setIsEditing(false);
    },
  });

  const addMemberMutation = useMutation({
    mutationFn: (userId: string) => APIClient.addGroupMember(groupId!, userId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['groups', groupId, 'members'] });
      queryClient.invalidateQueries({ queryKey: ['groups'] });
      setIsAddMemberOpen(false);
      setSelectedUserId('');
    },
  });

  const removeMemberMutation = useMutation({
    mutationFn: (userId: string) => APIClient.removeGroupMember(groupId!, userId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['groups', groupId, 'members'] });
      queryClient.invalidateQueries({ queryKey: ['groups'] });
    },
  });

  const handleRemoveMember = (member: GroupMember) => {
    ModalManager.confirmDelete(
      member.username || member.userId,
      'member',
      async () => { await removeMemberMutation.mutateAsync(member.userId); }
    );
  };

  const handleEditStart = () => {
    setEditForm({
      displayName: group?.displayName || '',
      description: group?.description || '',
    });
    setIsEditing(true);
  };

  const handleEditSave = () => {
    updateMutation.mutate(editForm);
  };

  // Users not already in the group
  const availableUsers = allUsers.filter(
    (u: User) => !members.some((m) => m.userId === u.id)
  );

  if (groupLoading) return <Loading />;
  if (!group) return <div className="p-6 text-red-500">Group not found.</div>;

  return (
    <div className="p-6 space-y-6">
      {/* Header */}
      <div className="flex items-center gap-4">
        <Button variant="ghost" size="sm" onClick={() => navigate('/groups')}>
          <ArrowLeft className="w-4 h-4" />
        </Button>
        <div className="flex-1">
          <h1 className="text-2xl font-bold text-gray-900 dark:text-white flex items-center gap-2">
            <Users className="w-6 h-6" />
            {group.displayName || group.name}
          </h1>
          <p className="text-sm text-gray-500 dark:text-gray-400">{group.name}</p>
        </div>
        {isAnyAdmin && !isEditing && (
          <Button variant="secondary" size="sm" onClick={handleEditStart}>
            Edit
          </Button>
        )}
      </div>

      {/* Edit Form */}
      {isEditing && (
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-4 space-y-4">
          <h2 className="font-medium text-gray-900 dark:text-white">Edit Group</h2>
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              Display Name
            </label>
            <Input
              value={editForm.displayName}
              onChange={(e) => setEditForm({ ...editForm, displayName: e.target.value })}
            />
          </div>
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              Description
            </label>
            <Input
              value={editForm.description}
              onChange={(e) => setEditForm({ ...editForm, description: e.target.value })}
            />
          </div>
          <div className="flex gap-3">
            <Button onClick={handleEditSave} disabled={updateMutation.isPending}>
              <Save className="w-4 h-4 mr-1" />
              {updateMutation.isPending ? 'Saving...' : 'Save'}
            </Button>
            <Button variant="secondary" onClick={() => setIsEditing(false)}>
              Cancel
            </Button>
          </div>
        </div>
      )}

      {/* Info Card */}
      {!isEditing && (
        <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 p-4">
          <dl className="grid grid-cols-2 gap-4 sm:grid-cols-3">
            <div>
              <dt className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wider">Members</dt>
              <dd className="mt-1 font-semibold text-gray-900 dark:text-white">{members.length}</dd>
            </div>
            {group.description && (
              <div className="col-span-2">
                <dt className="text-xs text-gray-500 dark:text-gray-400 uppercase tracking-wider">Description</dt>
                <dd className="mt-1 text-gray-700 dark:text-gray-300">{group.description}</dd>
              </div>
            )}
          </dl>
        </div>
      )}

      {/* Members */}
      <div>
        <div className="flex items-center justify-between mb-3">
          <h2 className="font-semibold text-gray-900 dark:text-white">Members</h2>
          {isAnyAdmin && (
            <Button size="sm" onClick={() => setIsAddMemberOpen(true)} className="flex items-center gap-2">
              <Plus className="w-4 h-4" />
              Add Member
            </Button>
          )}
        </div>

        {membersLoading ? (
          <Loading />
        ) : members.length === 0 ? (
          <div className="text-center py-8 text-gray-400 dark:text-gray-500 text-sm">
            No members yet. Add users to this group to grant them bucket permissions.
          </div>
        ) : (
          <div className="bg-white dark:bg-gray-800 rounded-lg border border-gray-200 dark:border-gray-700 overflow-hidden">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>User</TableHead>
                  <TableHead>Email</TableHead>
                  <TableHead>Added</TableHead>
                  {isAnyAdmin && <TableHead className="text-right">Actions</TableHead>}
                </TableRow>
              </TableHeader>
              <TableBody>
                {members.map((member) => (
                  <TableRow key={member.userId}>
                    <TableCell className="font-medium">{member.username || member.userId}</TableCell>
                    <TableCell className="text-gray-500 dark:text-gray-400">
                      {member.email || '—'}
                    </TableCell>
                    <TableCell className="text-gray-500 dark:text-gray-400 text-sm">
                      {formatRelativeTime(member.addedAt * 1000)}
                    </TableCell>
                    {isAnyAdmin && (
                      <TableCell className="text-right">
                        <Button
                          size="sm"
                          variant="ghost"
                          onClick={() => handleRemoveMember(member)}
                          className="text-red-500 hover:text-red-600 hover:bg-red-50 dark:hover:bg-red-900/20"
                        >
                          <UserMinus className="w-4 h-4" />
                        </Button>
                      </TableCell>
                    )}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </div>

      {/* Add Member Modal */}
      <Modal
        isOpen={isAddMemberOpen}
        onClose={() => { setIsAddMemberOpen(false); setSelectedUserId(''); }}
        title="Add Member"
      >
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">
              Select User
            </label>
            <select
              className="w-full rounded-md border border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-700 text-gray-900 dark:text-white px-3 py-2 text-sm"
              value={selectedUserId}
              onChange={(e) => setSelectedUserId(e.target.value)}
            >
              <option value="">— Select a user —</option>
              {availableUsers.map((u: User) => (
                <option key={u.id} value={u.id}>
                  {u.username} {u.email ? `(${u.email})` : ''}
                </option>
              ))}
            </select>
          </div>
          <div className="flex justify-end gap-3">
            <Button variant="secondary" onClick={() => { setIsAddMemberOpen(false); setSelectedUserId(''); }}>
              Cancel
            </Button>
            <Button
              onClick={() => addMemberMutation.mutate(selectedUserId)}
              disabled={!selectedUserId || addMemberMutation.isPending}
            >
              {addMemberMutation.isPending ? 'Adding...' : 'Add Member'}
            </Button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
