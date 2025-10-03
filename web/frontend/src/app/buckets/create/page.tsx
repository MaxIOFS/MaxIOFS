'use client';

import React, { useState } from 'react';
import { useRouter } from 'next/navigation';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/Card';
import { Loading } from '@/components/ui/Loading';
import SweetAlert from '@/lib/sweetalert';
import {
  ArrowLeft,
  Database,
  Lock,
  Shield,
  Clock,
  Settings,
  AlertTriangle,
  Info,
  CheckCircle2
} from 'lucide-react';
import { useMutation } from '@tanstack/react-query';
import { APIClient } from '@/lib/api';

interface BucketCreationConfig {
  // General
  name: string;
  region: string;
  
  // Versioning
  versioningEnabled: boolean;
  
  // Object Lock & WORM
  objectLockEnabled: boolean;
  retentionMode: 'GOVERNANCE' | 'COMPLIANCE' | '';
  retentionDays: number;
  retentionYears: number;
  
  // Encryption
  encryptionEnabled: boolean;
  encryptionType: 'AES256' | 'aws:kms';
  kmsKeyId: string;
  
  // Access Control
  blockPublicAccess: boolean;
  blockPublicAcls: boolean;
  ignorePublicAcls: boolean;
  blockPublicPolicy: boolean;
  restrictPublicBuckets: boolean;
  
  // Lifecycle
  lifecycleEnabled: boolean;
  transitionToIA: number; // días hasta IA
  transitionToGlacier: number; // días hasta Glacier
  expirationDays: number;
  
  // Advanced
  requesterPays: boolean;
  transferAcceleration: boolean;
  
  // Tags
  tags: Array<{ key: string; value: string }>;
}

export default function CreateBucketPage() {
  const router = useRouter();
  const [activeTab, setActiveTab] = useState<'general' | 'objectlock' | 'lifecycle' | 'encryption' | 'access'>('general');
  const [config, setConfig] = useState<BucketCreationConfig>({
    name: '',
    region: 'us-east-1',
    versioningEnabled: false,
    objectLockEnabled: false,
    retentionMode: '',
    retentionDays: 0,
    retentionYears: 0,
    encryptionEnabled: true,
    encryptionType: 'AES256',
    kmsKeyId: '',
    blockPublicAccess: true,
    blockPublicAcls: true,
    ignorePublicAcls: true,
    blockPublicPolicy: true,
    restrictPublicBuckets: true,
    lifecycleEnabled: false,
    transitionToIA: 30,
    transitionToGlacier: 90,
    expirationDays: 365,
    requesterPays: false,
    transferAcceleration: false,
    tags: [],
  });

  const createBucketMutation = useMutation({
    mutationFn: async () => {
      // Construct the creation payload
      const payload: any = {
        name: config.name,
        region: config.region,
        versioning: config.versioningEnabled ? { status: 'Enabled' } : undefined,
        encryption: config.encryptionEnabled ? {
          type: config.encryptionType,
          kmsKeyId: config.encryptionType === 'aws:kms' ? config.kmsKeyId : undefined,
        } : undefined,
        objectLock: config.objectLockEnabled ? {
          enabled: true,
          mode: config.retentionMode,
          days: config.retentionDays > 0 ? config.retentionDays : undefined,
          years: config.retentionYears > 0 ? config.retentionYears : undefined,
        } : undefined,
        publicAccessBlock: {
          blockPublicAcls: config.blockPublicAcls,
          ignorePublicAcls: config.ignorePublicAcls,
          blockPublicPolicy: config.blockPublicPolicy,
          restrictPublicBuckets: config.restrictPublicBuckets,
        },
        lifecycle: config.lifecycleEnabled ? {
          rules: [
            ...(config.transitionToIA > 0 ? [{
              id: 'transition-to-ia',
              status: 'Enabled',
              transition: {
                days: config.transitionToIA,
                storageClass: 'STANDARD_IA',
              },
            }] : []),
            ...(config.transitionToGlacier > 0 ? [{
              id: 'transition-to-glacier',
              status: 'Enabled',
              transition: {
                days: config.transitionToGlacier,
                storageClass: 'GLACIER',
              },
            }] : []),
            ...(config.expirationDays > 0 ? [{
              id: 'expiration',
              status: 'Enabled',
              expiration: {
                days: config.expirationDays,
              },
            }] : []),
          ],
        } : undefined,
        tags: config.tags.length > 0 ? config.tags : undefined,
      };

      return APIClient.createBucket(payload);
    },
    onSuccess: () => {
      SweetAlert.toast('success', `Bucket "${config.name}" creado exitosamente`);
      router.push('/buckets');
    },
    onError: (error) => {
      SweetAlert.apiError(error);
    },
  });

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    
    // Validations
    if (!config.name) {
      SweetAlert.toast('error', 'El nombre del bucket es requerido');
      return;
    }
    
    if (!/^[a-z0-9][a-z0-9.-]*[a-z0-9]$/.test(config.name)) {
      SweetAlert.toast('error', 'Nombre de bucket inválido. Debe contener solo minúsculas, números, puntos y guiones');
      return;
    }
    
    if (config.objectLockEnabled && !config.versioningEnabled) {
      SweetAlert.toast('error', 'Object Lock requiere que el versionado esté habilitado');
      return;
    }
    
    if (config.objectLockEnabled && !config.retentionMode) {
      SweetAlert.toast('error', 'Debe seleccionar un modo de retención para Object Lock');
      return;
    }
    
    if (config.objectLockEnabled && config.retentionDays === 0 && config.retentionYears === 0) {
      SweetAlert.toast('error', 'Debe especificar al menos días o años de retención');
      return;
    }

    const result = await SweetAlert.fire({
      icon: 'question',
      title: '¿Crear bucket?',
      html: `
        <div class="text-left space-y-2">
          <p><strong>Nombre:</strong> ${config.name}</p>
          <p><strong>Región:</strong> ${config.region}</p>
          ${config.objectLockEnabled ? `
            <p class="text-yellow-600"><strong>⚠️ Object Lock:</strong> ${config.retentionMode}</p>
            <p class="text-sm text-red-600">Este bucket será INMUTABLE y no se podrá eliminar hasta que expire la retención</p>
          ` : ''}
          ${config.versioningEnabled ? '<p><strong>✓</strong> Versionado habilitado</p>' : ''}
          ${config.encryptionEnabled ? '<p><strong>✓</strong> Encriptación habilitada</p>' : ''}
        </div>
      `,
      showCancelButton: true,
      confirmButtonText: 'Crear Bucket',
      cancelButtonText: 'Cancelar',
    });

    if (result.isConfirmed) {
      SweetAlert.loading('Creando bucket...', `Configurando "${config.name}"`);
      createBucketMutation.mutate();
    }
  };

  const updateConfig = (key: keyof BucketCreationConfig, value: any) => {
    setConfig(prev => ({ ...prev, [key]: value }));
  };

  const addTag = () => {
    setConfig(prev => ({
      ...prev,
      tags: [...prev.tags, { key: '', value: '' }],
    }));
  };

  const removeTag = (index: number) => {
    setConfig(prev => ({
      ...prev,
      tags: prev.tags.filter((_, i) => i !== index),
    }));
  };

  const updateTag = (index: number, field: 'key' | 'value', value: string) => {
    setConfig(prev => ({
      ...prev,
      tags: prev.tags.map((tag, i) => i === index ? { ...tag, [field]: value } : tag),
    }));
  };

  const tabs = [
    { id: 'general', label: 'General', icon: Database },
    { id: 'objectlock', label: 'Object Lock & WORM', icon: Lock },
    { id: 'lifecycle', label: 'Lifecycle', icon: Clock },
    { id: 'encryption', label: 'Encriptación', icon: Shield },
    { id: 'access', label: 'Control de Acceso', icon: Settings },
  ];

  return (
    <div className="space-y-6 p-8">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => router.push('/buckets')}
            className="gap-2"
          >
            <ArrowLeft className="h-4 w-4" />
            Volver
          </Button>
          <div>
            <h1 className="text-3xl font-bold tracking-tight">Crear Nuevo Bucket</h1>
            <p className="text-muted-foreground">
              Configura todas las opciones avanzadas para tu nuevo bucket S3
            </p>
          </div>
        </div>
      </div>

      <form onSubmit={handleSubmit}>
        {/* Tabs */}
        <div className="border-b border-gray-200 mb-6">
          <nav className="-mb-px flex space-x-8">
            {tabs.map((tab) => {
              const Icon = tab.icon;
              return (
                <button
                  key={tab.id}
                  type="button"
                  onClick={() => setActiveTab(tab.id as any)}
                  className={`
                    flex items-center gap-2 py-4 px-1 border-b-2 font-medium text-sm
                    ${activeTab === tab.id
                      ? 'border-blue-500 text-blue-600'
                      : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'
                    }
                  `}
                >
                  <Icon className="h-5 w-5" />
                  {tab.label}
                </button>
              );
            })}
          </nav>
        </div>

        {/* Tab Content */}
        <div className="space-y-6">
          {/* General Tab */}
          {activeTab === 'general' && (
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Database className="h-5 w-5" />
                  Configuración General
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div>
                  <label className="block text-sm font-medium mb-2">
                    Nombre del Bucket <span className="text-red-500">*</span>
                  </label>
                  <Input
                    value={config.name}
                    onChange={(e) => updateConfig('name', e.target.value.toLowerCase())}
                    placeholder="mi-bucket-s3"
                    required
                  />
                  <p className="text-xs text-muted-foreground mt-1">
                    Solo minúsculas, números, puntos (.) y guiones (-). Debe ser único globalmente.
                  </p>
                </div>

                <div>
                  <label className="block text-sm font-medium mb-2">Región</label>
                  <select
                    value={config.region}
                    onChange={(e) => updateConfig('region', e.target.value)}
                    className="w-full px-3 py-2 border border-input bg-background rounded-md"
                  >
                    <option value="us-east-1">US East (N. Virginia)</option>
                    <option value="us-west-2">US West (Oregon)</option>
                    <option value="eu-west-1">EU (Ireland)</option>
                    <option value="ap-southeast-1">Asia Pacific (Singapore)</option>
                  </select>
                </div>

                <div className="flex items-center space-x-2">
                  <input
                    type="checkbox"
                    id="versioning"
                    checked={config.versioningEnabled}
                    onChange={(e) => updateConfig('versioningEnabled', e.target.checked)}
                    className="rounded border-gray-300"
                  />
                  <label htmlFor="versioning" className="text-sm font-medium">
                    Habilitar Versionado de Objetos
                  </label>
                </div>
                <p className="text-xs text-muted-foreground ml-6">
                  Mantiene múltiples versiones de cada objeto. Requerido para Object Lock.
                </p>

                <div>
                  <label className="block text-sm font-medium mb-2">Tags</label>
                  <div className="space-y-2">
                    {config.tags.map((tag, index) => (
                      <div key={index} className="flex gap-2">
                        <Input
                          placeholder="Clave"
                          value={tag.key}
                          onChange={(e) => updateTag(index, 'key', e.target.value)}
                          className="flex-1"
                        />
                        <Input
                          placeholder="Valor"
                          value={tag.value}
                          onChange={(e) => updateTag(index, 'value', e.target.value)}
                          className="flex-1"
                        />
                        <Button
                          type="button"
                          variant="ghost"
                          onClick={() => removeTag(index)}
                        >
                          ✕
                        </Button>
                      </div>
                    ))}
                    <Button
                      type="button"
                      variant="outline"
                      onClick={addTag}
                      className="w-full"
                    >
                      + Agregar Tag
                    </Button>
                  </div>
                </div>
              </CardContent>
            </Card>
          )}

          {/* Object Lock Tab */}
          {activeTab === 'objectlock' && (
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Lock className="h-5 w-5" />
                  Object Lock & WORM (Write Once Read Many)
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="bg-yellow-50 border border-yellow-200 rounded-md p-4">
                  <div className="flex gap-2">
                    <AlertTriangle className="h-5 w-5 text-yellow-600 flex-shrink-0" />
                    <div className="text-sm text-yellow-800">
                      <p className="font-semibold mb-1">⚠️ Importante: Object Lock es PERMANENTE</p>
                      <ul className="list-disc list-inside space-y-1">
                        <li>Una vez habilitado, NO SE PUEDE DESHABILITAR</li>
                        <li>Los objetos no se pueden eliminar hasta que expire su período de retención</li>
                        <li>COMPLIANCE mode: Ni siquiera el root user puede eliminar objetos</li>
                        <li>GOVERNANCE mode: Solo usuarios con permisos especiales pueden bypass</li>
                      </ul>
                    </div>
                  </div>
                </div>

                <div className="flex items-center space-x-2">
                  <input
                    type="checkbox"
                    id="objectLock"
                    checked={config.objectLockEnabled}
                    onChange={(e) => {
                      updateConfig('objectLockEnabled', e.target.checked);
                      if (e.target.checked) {
                        updateConfig('versioningEnabled', true);
                      }
                    }}
                    className="rounded border-gray-300"
                  />
                  <label htmlFor="objectLock" className="text-sm font-medium">
                    Habilitar Object Lock (WORM)
                  </label>
                </div>

                {config.objectLockEnabled && (
                  <>
                    <div>
                      <label className="block text-sm font-medium mb-2">
                        Modo de Retención <span className="text-red-500">*</span>
                      </label>
                      <div className="space-y-3">
                        <label className="flex items-start space-x-3 p-3 border rounded-md cursor-pointer hover:bg-gray-50">
                          <input
                            type="radio"
                            name="retentionMode"
                            value="COMPLIANCE"
                            checked={config.retentionMode === 'COMPLIANCE'}
                            onChange={(e) => updateConfig('retentionMode', e.target.value)}
                            className="mt-1"
                          />
                          <div>
                            <div className="font-medium text-sm">COMPLIANCE (Cumplimiento Normativo)</div>
                            <div className="text-xs text-muted-foreground mt-1">
                              <strong>Máxima protección.</strong> Nadie puede eliminar o modificar objetos, ni siquiera el root user.
                              Ideal para requisitos legales y regulatorios (SEC, FINRA, HIPAA).
                            </div>
                          </div>
                        </label>

                        <label className="flex items-start space-x-3 p-3 border rounded-md cursor-pointer hover:bg-gray-50">
                          <input
                            type="radio"
                            name="retentionMode"
                            value="GOVERNANCE"
                            checked={config.retentionMode === 'GOVERNANCE'}
                            onChange={(e) => updateConfig('retentionMode', e.target.value)}
                            className="mt-1"
                          />
                          <div>
                            <div className="font-medium text-sm">GOVERNANCE (Gobernanza)</div>
                            <div className="text-xs text-muted-foreground mt-1">
                              <strong>Protección flexible.</strong> Usuarios con permisos especiales pueden bypass la retención.
                              Útil para testing y escenarios donde se necesita flexibilidad.
                            </div>
                          </div>
                        </label>
                      </div>
                    </div>

                    <div className="grid grid-cols-2 gap-4">
                      <div>
                        <label className="block text-sm font-medium mb-2">Días de Retención</label>
                        <Input
                          type="number"
                          min="0"
                          value={config.retentionDays}
                          onChange={(e) => updateConfig('retentionDays', parseInt(e.target.value) || 0)}
                          placeholder="0"
                        />
                      </div>
                      <div>
                        <label className="block text-sm font-medium mb-2">Años de Retención</label>
                        <Input
                          type="number"
                          min="0"
                          value={config.retentionYears}
                          onChange={(e) => updateConfig('retentionYears', parseInt(e.target.value) || 0)}
                          placeholder="0"
                        />
                      </div>
                    </div>
                    <p className="text-xs text-muted-foreground">
                      Especifica al menos uno. Los objetos no se pueden eliminar durante este período.
                    </p>

                    {config.retentionMode === 'COMPLIANCE' && (
                      <div className="bg-red-50 border border-red-200 rounded-md p-3 text-sm text-red-800">
                        <strong>⚠️ Modo COMPLIANCE seleccionado:</strong> Este bucket será INMUTABLE.
                        Los objetos no se podrán eliminar bajo ninguna circunstancia hasta que expire la retención.
                      </div>
                    )}
                  </>
                )}
              </CardContent>
            </Card>
          )}

          {/* Lifecycle Tab */}
          {activeTab === 'lifecycle' && (
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Clock className="h-5 w-5" />
                  Políticas de Ciclo de Vida
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="flex items-center space-x-2">
                  <input
                    type="checkbox"
                    id="lifecycle"
                    checked={config.lifecycleEnabled}
                    onChange={(e) => updateConfig('lifecycleEnabled', e.target.checked)}
                    className="rounded border-gray-300"
                  />
                  <label htmlFor="lifecycle" className="text-sm font-medium">
                    Habilitar Reglas de Ciclo de Vida
                  </label>
                </div>

                {config.lifecycleEnabled && (
                  <>
                    <div>
                      <label className="block text-sm font-medium mb-2">
                        Transición a Standard-IA (días)
                      </label>
                      <Input
                        type="number"
                        min="0"
                        value={config.transitionToIA}
                        onChange={(e) => updateConfig('transitionToIA', parseInt(e.target.value) || 0)}
                        placeholder="30"
                      />
                      <p className="text-xs text-muted-foreground mt-1">
                        Objetos se mueven a almacenamiento de acceso infrecuente después de N días
                      </p>
                    </div>

                    <div>
                      <label className="block text-sm font-medium mb-2">
                        Transición a Glacier (días)
                      </label>
                      <Input
                        type="number"
                        min="0"
                        value={config.transitionToGlacier}
                        onChange={(e) => updateConfig('transitionToGlacier', parseInt(e.target.value) || 0)}
                        placeholder="90"
                      />
                      <p className="text-xs text-muted-foreground mt-1">
                        Objetos se archivan en Glacier después de N días (almacenamiento económico)
                      </p>
                    </div>

                    <div>
                      <label className="block text-sm font-medium mb-2">
                        Expiración (días)
                      </label>
                      <Input
                        type="number"
                        min="0"
                        value={config.expirationDays}
                        onChange={(e) => updateConfig('expirationDays', parseInt(e.target.value) || 0)}
                        placeholder="365"
                      />
                      <p className="text-xs text-muted-foreground mt-1">
                        Objetos se eliminan permanentemente después de N días (0 = no expirar)
                      </p>
                    </div>
                  </>
                )}
              </CardContent>
            </Card>
          )}

          {/* Encryption Tab */}
          {activeTab === 'encryption' && (
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Shield className="h-5 w-5" />
                  Encriptación
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="flex items-center space-x-2">
                  <input
                    type="checkbox"
                    id="encryption"
                    checked={config.encryptionEnabled}
                    onChange={(e) => updateConfig('encryptionEnabled', e.target.checked)}
                    className="rounded border-gray-300"
                  />
                  <label htmlFor="encryption" className="text-sm font-medium">
                    Habilitar Encriptación por Defecto
                  </label>
                </div>

                {config.encryptionEnabled && (
                  <>
                    <div>
                      <label className="block text-sm font-medium mb-2">Tipo de Encriptación</label>
                      <div className="space-y-2">
                        <label className="flex items-center space-x-2">
                          <input
                            type="radio"
                            name="encryptionType"
                            value="AES256"
                            checked={config.encryptionType === 'AES256'}
                            onChange={(e) => updateConfig('encryptionType', e.target.value)}
                          />
                          <span className="text-sm">
                            <strong>SSE-S3 (AES-256)</strong> - Encriptación administrada por S3
                          </span>
                        </label>
                        <label className="flex items-center space-x-2">
                          <input
                            type="radio"
                            name="encryptionType"
                            value="aws:kms"
                            checked={config.encryptionType === 'aws:kms'}
                            onChange={(e) => updateConfig('encryptionType', e.target.value)}
                          />
                          <span className="text-sm">
                            <strong>SSE-KMS</strong> - Encriptación con AWS Key Management Service
                          </span>
                        </label>
                      </div>
                    </div>

                    {config.encryptionType === 'aws:kms' && (
                      <div>
                        <label className="block text-sm font-medium mb-2">KMS Key ID</label>
                        <Input
                          value={config.kmsKeyId}
                          onChange={(e) => updateConfig('kmsKeyId', e.target.value)}
                          placeholder="arn:aws:kms:region:account:key/key-id"
                        />
                      </div>
                    )}
                  </>
                )}
              </CardContent>
            </Card>
          )}

          {/* Access Control Tab */}
          {activeTab === 'access' && (
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Settings className="h-5 w-5" />
                  Control de Acceso Público
                </CardTitle>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="bg-blue-50 border border-blue-200 rounded-md p-3 text-sm text-blue-800">
                  <Info className="h-4 w-4 inline mr-2" />
                  Se recomienda bloquear todo el acceso público a menos que específicamente necesites compartir datos.
                </div>

                <div className="space-y-3">
                  <label className="flex items-center space-x-2">
                    <input
                      type="checkbox"
                      checked={config.blockPublicAcls}
                      onChange={(e) => updateConfig('blockPublicAcls', e.target.checked)}
                      className="rounded border-gray-300"
                    />
                    <span className="text-sm">Bloquear ACLs públicas</span>
                  </label>

                  <label className="flex items-center space-x-2">
                    <input
                      type="checkbox"
                      checked={config.ignorePublicAcls}
                      onChange={(e) => updateConfig('ignorePublicAcls', e.target.checked)}
                      className="rounded border-gray-300"
                    />
                    <span className="text-sm">Ignorar ACLs públicas existentes</span>
                  </label>

                  <label className="flex items-center space-x-2">
                    <input
                      type="checkbox"
                      checked={config.blockPublicPolicy}
                      onChange={(e) => updateConfig('blockPublicPolicy', e.target.checked)}
                      className="rounded border-gray-300"
                    />
                    <span className="text-sm">Bloquear políticas de bucket públicas</span>
                  </label>

                  <label className="flex items-center space-x-2">
                    <input
                      type="checkbox"
                      checked={config.restrictPublicBuckets}
                      onChange={(e) => updateConfig('restrictPublicBuckets', e.target.checked)}
                      className="rounded border-gray-300"
                    />
                    <span className="text-sm">Restringir buckets públicos</span>
                  </label>
                </div>

                <div className="border-t pt-4 mt-4">
                  <h3 className="font-medium mb-3">Opciones Avanzadas</h3>
                  
                  <div className="space-y-3">
                    <label className="flex items-center space-x-2">
                      <input
                        type="checkbox"
                        checked={config.requesterPays}
                        onChange={(e) => updateConfig('requesterPays', e.target.checked)}
                        className="rounded border-gray-300"
                      />
                      <div>
                        <div className="text-sm font-medium">Requester Pays</div>
                        <div className="text-xs text-muted-foreground">
                          El solicitante paga las transferencias de datos
                        </div>
                      </div>
                    </label>

                    <label className="flex items-center space-x-2">
                      <input
                        type="checkbox"
                        checked={config.transferAcceleration}
                        onChange={(e) => updateConfig('transferAcceleration', e.target.checked)}
                        className="rounded border-gray-300"
                      />
                      <div>
                        <div className="text-sm font-medium">Transfer Acceleration</div>
                        <div className="text-xs text-muted-foreground">
                          Acelera transferencias usando CloudFront
                        </div>
                      </div>
                    </label>
                  </div>
                </div>
              </CardContent>
            </Card>
          )}
        </div>

        {/* Action Buttons */}
        <div className="flex items-center justify-end gap-4 mt-8 pt-6 border-t">
          <Button
            type="button"
            variant="outline"
            onClick={() => router.push('/buckets')}
          >
            Cancelar
          </Button>
          <Button
            type="submit"
            disabled={createBucketMutation.isPending}
            className="gap-2"
          >
            {createBucketMutation.isPending ? (
              <>
                <Loading size="sm" />
                Creando...
              </>
            ) : (
              <>
                <CheckCircle2 className="h-4 w-4" />
                Crear Bucket
              </>
            )}
          </Button>
        </div>
      </form>
    </div>
  );
}
