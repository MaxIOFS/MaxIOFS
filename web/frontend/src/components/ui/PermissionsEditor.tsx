import React, { useState } from 'react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import {
  Shield,
  Plus,
  Trash2,
  User,
  Users,
  Globe,
  Lock,
  Eye,
  Edit,
  Folder,
  File,
  CheckCircle,
  XCircle
} from 'lucide-react';

export interface Permission {
  id: string;
  resource: string;
  actions: string[];
  principal: {
    type: 'user' | 'group' | 'role' | 'public';
    value: string;
  };
  effect: 'Allow' | 'Deny';
  conditions?: {
    [key: string]: any;
  };
}

export interface PermissionsEditorProps {
  permissions: Permission[];
  onChange: (permissions: Permission[]) => void;
  resourceType?: 'bucket' | 'object' | 'system';
  disabled?: boolean;
}

const S3_ACTIONS = {
  bucket: [
    's3:ListBucket',
    's3:GetBucketLocation',
    's3:GetBucketPolicy',
    's3:PutBucketPolicy',
    's3:DeleteBucketPolicy',
    's3:GetBucketCORS',
    's3:PutBucketCORS',
    's3:DeleteBucketCORS',
    's3:GetBucketVersioning',
    's3:PutBucketVersioning',
    's3:DeleteBucket'
  ],
  object: [
    's3:GetObject',
    's3:PutObject',
    's3:DeleteObject',
    's3:GetObjectACL',
    's3:PutObjectACL',
    's3:GetObjectVersion',
    's3:DeleteObjectVersion',
    's3:GetObjectTagging',
    's3:PutObjectTagging',
    's3:DeleteObjectTagging'
  ],
  system: [
    'admin:ListUsers',
    'admin:CreateUser',
    'admin:UpdateUser',
    'admin:DeleteUser',
    'admin:ListAccessKeys',
    'admin:CreateAccessKey',
    'admin:DeleteAccessKey',
    'admin:GetSystemConfig',
    'admin:UpdateSystemConfig',
    'admin:ViewMetrics'
  ]
};

export function PermissionsEditor({
  permissions,
  onChange,
  resourceType = 'bucket',
  disabled = false
}: PermissionsEditorProps) {
  const [isAddingPermission, setIsAddingPermission] = useState(false);
  const [newPermission, setNewPermission] = useState<Partial<Permission>>({
    resource: '*',
    actions: [],
    principal: { type: 'user', value: '' },
    effect: 'Allow'
  });

  const availableActions = S3_ACTIONS[resourceType] || S3_ACTIONS.bucket;

  const addPermission = () => {
    if (!newPermission.principal?.value || newPermission.actions?.length === 0) {
      return;
    }

    const permission: Permission = {
      id: Math.random().toString(36).substr(2, 9),
      resource: newPermission.resource || '*',
      actions: newPermission.actions || [],
      principal: newPermission.principal as Permission['principal'],
      effect: newPermission.effect as 'Allow' | 'Deny'
    };

    onChange([...permissions, permission]);
    setNewPermission({
      resource: '*',
      actions: [],
      principal: { type: 'user', value: '' },
      effect: 'Allow'
    });
    setIsAddingPermission(false);
  };

  const removePermission = (id: string) => {
    onChange(permissions.filter(p => p.id !== id));
  };

  const updateNewPermission = (field: string, value: any) => {
    setNewPermission(prev => ({ ...prev, [field]: value }));
  };

  const updateNewPrincipal = (field: string, value: any) => {
    setNewPermission(prev => ({
      ...prev,
      principal: { ...prev.principal!, [field]: value }
    }));
  };

  const toggleAction = (action: string) => {
    const currentActions = newPermission.actions || [];
    const updatedActions = currentActions.includes(action)
      ? currentActions.filter(a => a !== action)
      : [...currentActions, action];

    updateNewPermission('actions', updatedActions);
  };

  const getPrincipalIcon = (type: string) => {
    switch (type) {
      case 'user': return <User className="h-4 w-4" />;
      case 'group': return <Users className="h-4 w-4" />;
      case 'role': return <Shield className="h-4 w-4" />;
      case 'public': return <Globe className="h-4 w-4" />;
      default: return <User className="h-4 w-4" />;
    }
  };

  const getActionIcon = (action: string) => {
    if (action.includes('Get') || action.includes('List')) return <Eye className="h-3 w-3" />;
    if (action.includes('Put') || action.includes('Create')) return <Edit className="h-3 w-3" />;
    if (action.includes('Delete')) return <Trash2 className="h-3 w-3" />;
    if (action.includes('Bucket')) return <Folder className="h-3 w-3" />;
    if (action.includes('Object')) return <File className="h-3 w-3" />;
    return <Shield className="h-3 w-3" />;
  };

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between">
        <CardTitle className="flex items-center gap-2">
          <Shield className="h-5 w-5" />
          Permissions
        </CardTitle>
        {!disabled && (
          <Button
            onClick={() => setIsAddingPermission(true)}
            size="sm"
            className="gap-2"
          >
            <Plus className="h-4 w-4" />
            Add Permission
          </Button>
        )}
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Existing Permissions */}
        {permissions.length === 0 ? (
          <div className="text-center py-8 text-muted-foreground">
            <Shield className="h-12 w-12 mx-auto mb-4 text-muted-foreground" />
            <p>No permissions configured</p>
            {!disabled && (
              <Button
                onClick={() => setIsAddingPermission(true)}
                className="mt-4 gap-2"
                variant="outline"
              >
                <Plus className="h-4 w-4" />
                Add First Permission
              </Button>
            )}
          </div>
        ) : (
          <div className="space-y-3">
            {permissions.map((permission) => (
              <div
                key={permission.id}
                className="border rounded-lg p-4 space-y-3"
              >
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    {permission.effect === 'Allow' ? (
                      <CheckCircle className="h-4 w-4 text-green-600" />
                    ) : (
                      <XCircle className="h-4 w-4 text-red-600" />
                    )}
                    <span className={`font-medium ${
                      permission.effect === 'Allow' ? 'text-green-700' : 'text-red-700'
                    }`}>
                      {permission.effect}
                    </span>
                  </div>
                  {!disabled && (
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => removePermission(permission.id)}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  )}
                </div>

                <div className="grid grid-cols-3 gap-4 text-sm">
                  <div>
                    <label className="font-medium text-muted-foreground">Principal</label>
                    <div className="flex items-center gap-2 mt-1">
                      {getPrincipalIcon(permission.principal.type)}
                      <span>{permission.principal.value}</span>
                      <span className="text-xs text-muted-foreground">
                        ({permission.principal.type})
                      </span>
                    </div>
                  </div>

                  <div>
                    <label className="font-medium text-muted-foreground">Resource</label>
                    <div className="flex items-center gap-2 mt-1">
                      <Folder className="h-4 w-4" />
                      <span>{permission.resource}</span>
                    </div>
                  </div>

                  <div>
                    <label className="font-medium text-muted-foreground">Actions</label>
                    <div className="flex flex-wrap gap-1 mt-1">
                      {permission.actions.map((action) => (
                        <span
                          key={action}
                          className="inline-flex items-center gap-1 px-2 py-1 bg-blue-100 text-blue-800 text-xs rounded-full"
                        >
                          {getActionIcon(action)}
                          {action}
                        </span>
                      ))}
                    </div>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}

        {/* Add Permission Form */}
        {isAddingPermission && (
          <Card className="border-2 border-dashed">
            <CardHeader>
              <CardTitle className="text-lg">Add New Permission</CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium mb-2">Effect</label>
                  <select
                    value={newPermission.effect}
                    onChange={(e) => updateNewPermission('effect', e.target.value)}
                    className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring"
                  >
                    <option value="Allow">Allow</option>
                    <option value="Deny">Deny</option>
                  </select>
                </div>

                <div>
                  <label className="block text-sm font-medium mb-2">Principal Type</label>
                  <select
                    value={newPermission.principal?.type}
                    onChange={(e) => updateNewPrincipal('type', e.target.value)}
                    className="w-full px-3 py-2 border border-input bg-background rounded-md focus:outline-none focus:ring-2 focus:ring-ring"
                  >
                    <option value="user">User</option>
                    <option value="group">Group</option>
                    <option value="role">Role</option>
                    <option value="public">Public</option>
                  </select>
                </div>
              </div>

              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium mb-2">Principal</label>
                  <Input
                    value={newPermission.principal?.value || ''}
                    onChange={(e) => updateNewPrincipal('value', e.target.value)}
                    placeholder={
                      newPermission.principal?.type === 'public'
                        ? '*'
                        : `Enter ${newPermission.principal?.type} name`
                    }
                    disabled={newPermission.principal?.type === 'public'}
                  />
                </div>

                <div>
                  <label className="block text-sm font-medium mb-2">Resource</label>
                  <Input
                    value={newPermission.resource}
                    onChange={(e) => updateNewPermission('resource', e.target.value)}
                    placeholder="bucket/* or specific resource"
                  />
                </div>
              </div>

              <div>
                <label className="block text-sm font-medium mb-2">Actions</label>
                <div className="grid grid-cols-2 gap-2 max-h-48 overflow-y-auto">
                  {availableActions.map((action) => (
                    <label key={action} className="flex items-center space-x-2">
                      <input
                        type="checkbox"
                        checked={newPermission.actions?.includes(action) || false}
                        onChange={() => toggleAction(action)}
                        className="rounded border-gray-300"
                      />
                      <span className="text-sm flex items-center gap-1">
                        {getActionIcon(action)}
                        {action}
                      </span>
                    </label>
                  ))}
                </div>
              </div>

              <div className="flex justify-end space-x-2 pt-4">
                <Button
                  variant="outline"
                  onClick={() => setIsAddingPermission(false)}
                >
                  Cancel
                </Button>
                <Button
                  onClick={addPermission}
                  disabled={!newPermission.principal?.value || newPermission.actions?.length === 0}
                >
                  Add Permission
                </Button>
              </div>
            </CardContent>
          </Card>
        )}
      </CardContent>
    </Card>
  );
}