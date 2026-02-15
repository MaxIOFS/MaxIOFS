import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';
import { Modal } from '@/components/ui/Modal';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Globe, Shield, X } from 'lucide-react';
import type { IdentityProvider, IDPType, LDAPConfig, OAuth2Config } from '@/types';
import ModalManager from '@/lib/modals';

interface CreateIDPModalProps {
  idp?: IdentityProvider | null;
  onClose: () => void;
  onSuccess: () => void;
}

const defaultLDAP: LDAPConfig = {
  host: '',
  port: 389,
  security: 'none',
  bind_dn: '',
  bind_password: '',
  base_dn: '',
  user_search_base: '',
  user_filter: '(objectClass=person)',
  group_search_base: '',
  group_filter: '(objectClass=group)',
  attr_username: 'sAMAccountName',
  attr_email: 'mail',
  attr_display_name: 'displayName',
  attr_member_of: 'memberOf',
};

const defaultOAuth: OAuth2Config = {
  preset: 'custom',
  client_id: '',
  client_secret: '',
  auth_url: '',
  token_url: '',
  userinfo_url: '',
  scopes: ['openid', 'profile', 'email'],
  redirect_uri: '',
  claim_email: 'email',
  claim_name: 'name',
  claim_groups: '',
};

export function CreateIDPModal({ idp, onClose, onSuccess }: CreateIDPModalProps) {
  const isEdit = !!idp;
  const [name, setName] = useState(idp?.name || '');
  const [type, setType] = useState<IDPType>(idp?.type || 'ldap');
  const [status, setStatus] = useState(idp?.status || 'testing');
  const [ldapConfig, setLdapConfig] = useState<LDAPConfig>(idp?.config?.ldap || defaultLDAP);
  const [oauthConfig, setOauthConfig] = useState<OAuth2Config>(idp?.config?.oauth2 || defaultOAuth);

  const createMutation = useMutation({
    mutationFn: (data: any) => isEdit ? APIClient.updateIDP(idp!.id, data) : APIClient.createIDP(data),
    onSuccess: () => {
      ModalManager.success(isEdit ? 'Updated' : 'Created', `Identity provider ${isEdit ? 'updated' : 'created'} successfully`);
      onSuccess();
    },
    onError: (err: any) => {
      ModalManager.error('Error', err.message || 'Failed to save provider');
    },
  });

  const handlePresetChange = (preset: string) => {
    const updated = { ...oauthConfig, preset };
    if (preset === 'google') {
      updated.auth_url = 'https://accounts.google.com/o/oauth2/v2/auth';
      updated.token_url = 'https://oauth2.googleapis.com/token';
      updated.userinfo_url = 'https://www.googleapis.com/oauth2/v3/userinfo';
      updated.scopes = ['openid', 'profile', 'email'];
      updated.claim_email = 'email';
      updated.claim_name = 'name';
    } else if (preset === 'microsoft') {
      updated.auth_url = 'https://login.microsoftonline.com/common/oauth2/v2.0/authorize';
      updated.token_url = 'https://login.microsoftonline.com/common/oauth2/v2.0/token';
      updated.userinfo_url = 'https://graph.microsoft.com/oidc/userinfo';
      updated.scopes = ['openid', 'profile', 'email'];
    }
    setOauthConfig(updated);
  };

  const handleSubmit = () => {
    const data: any = {
      name,
      type,
      status,
      config: type === 'ldap' ? { ldap: ldapConfig } : { oauth2: oauthConfig },
    };
    createMutation.mutate(data);
  };

  return (
    <Modal isOpen onClose={onClose} title={isEdit ? 'Edit Identity Provider' : 'Add Identity Provider'} size="lg">
      <div className="space-y-6 max-h-[70vh] overflow-y-auto pr-2">
        {/* Basic Info */}
        <div className="space-y-4">
          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Name</label>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g., Corporate AD, Google SSO" />
          </div>

          {!isEdit && (
            <div>
              <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-2">Type</label>
              <div className="grid grid-cols-2 gap-3">
                <button
                  type="button"
                  onClick={() => setType('ldap')}
                  className={`flex items-center gap-3 p-4 rounded-xl border-2 transition-all ${
                    type === 'ldap'
                      ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20'
                      : 'border-gray-200 dark:border-gray-700 hover:border-gray-300'
                  }`}
                >
                  <Globe className={`h-6 w-6 ${type === 'ldap' ? 'text-blue-600' : 'text-gray-400'}`} />
                  <div className="text-left">
                    <p className="font-medium text-gray-900 dark:text-white text-sm">LDAP / AD</p>
                    <p className="text-xs text-gray-500">Active Directory, OpenLDAP</p>
                  </div>
                </button>
                <button
                  type="button"
                  onClick={() => setType('oauth2')}
                  className={`flex items-center gap-3 p-4 rounded-xl border-2 transition-all ${
                    type === 'oauth2'
                      ? 'border-purple-500 bg-purple-50 dark:bg-purple-900/20'
                      : 'border-gray-200 dark:border-gray-700 hover:border-gray-300'
                  }`}
                >
                  <Shield className={`h-6 w-6 ${type === 'oauth2' ? 'text-purple-600' : 'text-gray-400'}`} />
                  <div className="text-left">
                    <p className="font-medium text-gray-900 dark:text-white text-sm">OAuth2 / OIDC</p>
                    <p className="text-xs text-gray-500">Google, Microsoft, Custom</p>
                  </div>
                </button>
              </div>
            </div>
          )}

          <div>
            <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Status</label>
            <select
              value={status}
              onChange={(e) => setStatus(e.target.value as any)}
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-800 text-sm text-gray-900 dark:text-white"
            >
              <option value="testing">Testing</option>
              <option value="active">Active</option>
              <option value="inactive">Inactive</option>
            </select>
          </div>
        </div>

        {/* Type-specific config */}
        {type === 'ldap' ? (
          <LDAPFields config={ldapConfig} onChange={setLdapConfig} />
        ) : (
          <OAuthFields config={oauthConfig} onChange={setOauthConfig} onPresetChange={handlePresetChange} />
        )}
      </div>

      <div className="flex justify-end gap-3 mt-6 pt-4 border-t border-gray-200 dark:border-gray-700">
        <Button variant="secondary" onClick={onClose}>Cancel</Button>
        <Button onClick={handleSubmit} disabled={createMutation.isPending || !name}>
          {createMutation.isPending ? 'Saving...' : isEdit ? 'Update' : 'Create'}
        </Button>
      </div>
    </Modal>
  );
}

function LDAPFields({ config, onChange }: { config: LDAPConfig; onChange: (c: LDAPConfig) => void }) {
  const update = (key: keyof LDAPConfig, value: any) => onChange({ ...config, [key]: value });

  return (
    <div className="space-y-4">
      <h3 className="text-sm font-semibold text-gray-900 dark:text-white border-b border-gray-200 dark:border-gray-700 pb-2">Connection</h3>
      <div className="grid grid-cols-2 gap-4">
        <div>
          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Host</label>
          <Input value={config.host} onChange={(e) => update('host', e.target.value)} placeholder="ldap.company.com" />
        </div>
        <div className="grid grid-cols-2 gap-2">
          <div>
            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Port</label>
            <Input type="number" value={config.port} onChange={(e) => update('port', parseInt(e.target.value) || 389)} />
          </div>
          <div>
            <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Security</label>
            <select value={config.security} onChange={(e) => update('security', e.target.value)} className="w-full px-2 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-800 text-sm text-gray-900 dark:text-white">
              <option value="none">None</option>
              <option value="tls">TLS (LDAPS)</option>
              <option value="starttls">StartTLS</option>
            </select>
          </div>
        </div>
      </div>
      <div className="grid grid-cols-1 gap-4">
        <div>
          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Bind DN</label>
          <Input value={config.bind_dn} onChange={(e) => update('bind_dn', e.target.value)} placeholder="cn=readonly,dc=company,dc=com" />
        </div>
        <div>
          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Bind Password</label>
          <Input type="password" value={config.bind_password} onChange={(e) => update('bind_password', e.target.value)} placeholder="********" />
        </div>
      </div>

      <h3 className="text-sm font-semibold text-gray-900 dark:text-white border-b border-gray-200 dark:border-gray-700 pb-2 pt-2">Search</h3>
      <div>
        <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Base DN</label>
        <Input value={config.base_dn} onChange={(e) => update('base_dn', e.target.value)} placeholder="dc=company,dc=com" />
      </div>
      <div className="grid grid-cols-2 gap-4">
        <div>
          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">User Search Base</label>
          <Input value={config.user_search_base} onChange={(e) => update('user_search_base', e.target.value)} placeholder="ou=Users" />
        </div>
        <div>
          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">User Filter</label>
          <Input value={config.user_filter} onChange={(e) => update('user_filter', e.target.value)} placeholder="(objectClass=person)" />
        </div>
      </div>
      <div className="grid grid-cols-2 gap-4">
        <div>
          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Group Search Base</label>
          <Input value={config.group_search_base} onChange={(e) => update('group_search_base', e.target.value)} placeholder="ou=Groups" />
        </div>
        <div>
          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Group Filter</label>
          <Input value={config.group_filter} onChange={(e) => update('group_filter', e.target.value)} placeholder="(objectClass=group)" />
        </div>
      </div>

      <h3 className="text-sm font-semibold text-gray-900 dark:text-white border-b border-gray-200 dark:border-gray-700 pb-2 pt-2">Attribute Mapping</h3>
      <div className="grid grid-cols-2 gap-4">
        <div>
          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Username Attribute</label>
          <Input value={config.attr_username} onChange={(e) => update('attr_username', e.target.value)} placeholder="sAMAccountName" />
        </div>
        <div>
          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Email Attribute</label>
          <Input value={config.attr_email} onChange={(e) => update('attr_email', e.target.value)} placeholder="mail" />
        </div>
        <div>
          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Display Name Attribute</label>
          <Input value={config.attr_display_name} onChange={(e) => update('attr_display_name', e.target.value)} placeholder="displayName" />
        </div>
        <div>
          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Member Of Attribute</label>
          <Input value={config.attr_member_of} onChange={(e) => update('attr_member_of', e.target.value)} placeholder="memberOf" />
        </div>
      </div>
    </div>
  );
}

function OAuthFields({ config, onChange, onPresetChange }: { config: OAuth2Config; onChange: (c: OAuth2Config) => void; onPresetChange: (p: string) => void }) {
  const update = (key: keyof OAuth2Config, value: any) => onChange({ ...config, [key]: value });
  const callbackUrl = `${window.location.origin}/api/v1/auth/oauth/callback`;

  return (
    <div className="space-y-4">
      <div>
        <label className="block text-sm font-medium text-gray-700 dark:text-gray-300 mb-1">Preset</label>
        <select
          value={config.preset}
          onChange={(e) => onPresetChange(e.target.value)}
          className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-lg bg-white dark:bg-gray-800 text-sm text-gray-900 dark:text-white"
        >
          <option value="custom">Custom</option>
          <option value="google">Google</option>
          <option value="microsoft">Microsoft</option>
        </select>
      </div>

      <div className="grid grid-cols-2 gap-4">
        <div>
          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Client ID</label>
          <Input value={config.client_id} onChange={(e) => update('client_id', e.target.value)} placeholder="your-client-id" />
        </div>
        <div>
          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Client Secret</label>
          <Input type="password" value={config.client_secret} onChange={(e) => update('client_secret', e.target.value)} placeholder="your-client-secret" />
        </div>
      </div>

      <h3 className="text-sm font-semibold text-gray-900 dark:text-white border-b border-gray-200 dark:border-gray-700 pb-2 pt-2">Endpoints</h3>
      <div>
        <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Authorization URL</label>
        <Input value={config.auth_url} onChange={(e) => update('auth_url', e.target.value)} placeholder="https://provider.com/authorize" />
      </div>
      <div>
        <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Token URL</label>
        <Input value={config.token_url} onChange={(e) => update('token_url', e.target.value)} placeholder="https://provider.com/token" />
      </div>
      <div>
        <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">User Info URL</label>
        <Input value={config.userinfo_url} onChange={(e) => update('userinfo_url', e.target.value)} placeholder="https://provider.com/userinfo" />
      </div>
      <div>
        <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Redirect URI</label>
        <Input value={config.redirect_uri} onChange={(e) => update('redirect_uri', e.target.value)} placeholder={callbackUrl} />
        <p className="text-xs text-gray-500 dark:text-gray-400 mt-1">
          Leave empty to auto-detect: <code className="bg-gray-100 dark:bg-gray-700 px-1 rounded text-xs select-all cursor-pointer" title="Click to select">{callbackUrl}</code>
          <br />Configure this URL in your OAuth provider's redirect/callback URI settings.
        </p>
      </div>

      <h3 className="text-sm font-semibold text-gray-900 dark:text-white border-b border-gray-200 dark:border-gray-700 pb-2 pt-2">Claim Mapping</h3>
      <div className="grid grid-cols-3 gap-4">
        <div>
          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Email Claim</label>
          <Input value={config.claim_email} onChange={(e) => update('claim_email', e.target.value)} placeholder="email" />
        </div>
        <div>
          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Name Claim</label>
          <Input value={config.claim_name} onChange={(e) => update('claim_name', e.target.value)} placeholder="name" />
        </div>
        <div>
          <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Groups Claim</label>
          <Input value={config.claim_groups} onChange={(e) => update('claim_groups', e.target.value)} placeholder="groups (optional)" />
        </div>
      </div>

      <div>
        <label className="block text-xs font-medium text-gray-600 dark:text-gray-400 mb-1">Scopes (comma-separated)</label>
        <Input value={(config.scopes || []).join(', ')} onChange={(e) => update('scopes', e.target.value.split(',').map((s: string) => s.trim()).filter(Boolean))} placeholder="openid, profile, email" />
      </div>
    </div>
  );
}
