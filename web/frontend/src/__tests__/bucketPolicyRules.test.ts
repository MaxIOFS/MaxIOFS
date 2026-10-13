import { describe, it, expect } from 'vitest';
import {
  isAddressRange,
  isObjectAction,
  newRule,
  policyFromRules,
  ruleProblem,
  ruleResources,
  rulesFromPolicy,
  type PolicyRule,
} from '@/lib/bucketPolicyRules';

const bucket = 'photos';
const statement = (fields: Record<string, unknown>) => ({ Version: '2012-10-17', Statement: [{ Effect: 'Allow', ...fields }] });

describe('bucket policy rules', () => {
  it('give back the rules they were made from', () => {
    const rules: PolicyRule[] = [
      { ...newRule(), everyone: true, actions: ['s3:GetObject', 's3:ListBucket'] },
      { ...newRule(), users: ['arn:aws:iam:::user/ana', 'legacy-user-id'], actions: ['s3:PutObject'], prefix: 'uploads/' },
      { ...newRule(), effect: 'Deny', everyone: true, allActions: true, addresses: 'notFrom', sourceIps: ['10.0.0.0/8', '2001:db8::/32'] },
      { ...newRule(), effect: 'Deny', everyone: true, allActions: true, transport: 'insecure' },
      { ...newRule(), users: ['arn:aws:iam:::user/ops'], actions: ['s3:GetBucketAcl'], addresses: 'from', sourceIps: ['203.0.113.5'], transport: 'secure' },
    ];
    const policy = JSON.parse(JSON.stringify(policyFromRules(rules, bucket)));
    expect(rulesFromPolicy(policy, bucket)).toEqual(rules);
  });

  it('write the resources the actions apply to', () => {
    expect(ruleResources({ ...newRule(), actions: ['s3:GetObject'] }, bucket)).toEqual(['arn:aws:s3:::photos/*']);
    expect(ruleResources({ ...newRule(), actions: ['s3:ListBucket'] }, bucket)).toEqual(['arn:aws:s3:::photos']);
    expect(ruleResources({ ...newRule(), actions: ['s3:ListBucket', 's3:GetObject'] }, bucket))
      .toEqual(['arn:aws:s3:::photos', 'arn:aws:s3:::photos/*']);
    expect(ruleResources({ ...newRule(), allActions: true }, bucket)).toEqual(['arn:aws:s3:::photos', 'arn:aws:s3:::photos/*']);
    expect(ruleResources({ ...newRule(), actions: ['s3:GetObject'], prefix: 'public/' }, bucket)).toEqual(['arn:aws:s3:::photos/public/*']);
  });

  it('read the policies the previous editor templates wrote', () => {
    const publicRead = statement({ Principal: '*', Action: 's3:GetObject', Resource: 'arn:aws:s3:::photos/*' });
    expect(rulesFromPolicy(publicRead, bucket)).toEqual([{ ...newRule(), everyone: true, actions: ['s3:GetObject'] }]);
    const full = statement({ Principal: '*', Action: 's3:*', Resource: ['arn:aws:s3:::photos', 'arn:aws:s3:::photos/*'] });
    expect(rulesFromPolicy(full, bucket)).toEqual([{ ...newRule(), everyone: true, allActions: true }]);
    const harmless = statement({ Principal: { AWS: '*' }, Action: ['s3:GetObject'], Resource: ['arn:aws:s3:::photos', 'arn:aws:s3:::photos/*'] });
    expect(rulesFromPolicy(harmless, bucket)).toEqual([{ ...newRule(), everyone: true, actions: ['s3:GetObject'] }]);
  });

  it('refuse to show what the rules cannot say', () => {
    expect(rulesFromPolicy(null, bucket)).toEqual([]);
    expect(rulesFromPolicy({ success: true, data: {} } as never, bucket)).toBeNull();
    for (const policy of [
      statement({ NotPrincipal: { AWS: 'x' }, Action: 's3:GetObject', Resource: 'arn:aws:s3:::photos/*' }),
      statement({ Principal: { CanonicalUser: 'x' }, Action: 's3:GetObject', Resource: 'arn:aws:s3:::photos/*' }),
      statement({ Principal: '*', Action: 's3:Get*', Resource: 'arn:aws:s3:::photos/*' }),
      statement({ Principal: '*', Action: 's3:Get*', Resource: ['arn:aws:s3:::photos', 'arn:aws:s3:::photos/*'] }),
      statement({ Principal: '*', Action: 's3:GetObject', Resource: 'arn:aws:s3:::photos/*', NotResource: 'arn:aws:s3:::photos/private/*' }),
      statement({ Principal: { AWS: 'arn:aws:iam:::user/a', CanonicalUser: 'x' }, Action: 's3:GetObject', Resource: 'arn:aws:s3:::photos/*' }),
      statement({ Principal: '*', Action: 's3:GetObject', Resource: 'arn:aws:s3:::photos/report.pdf' }),
      statement({ Principal: '*', Action: 's3:*', Resource: 'arn:aws:s3:::photos/*' }),
      statement({ Principal: '*', Action: 's3:ListBucket', Resource: 'arn:aws:s3:::photos/*' }),
      statement({ Principal: '*', Action: 's3:ListBucket', Resource: 'arn:aws:s3:::photos/public/*' }),
      statement({ Principal: '*', Action: 's3:GetObject', Resource: 'arn:aws:s3:::photos/*', Condition: { StringEquals: { 'aws:SourceIp': '1.2.3.4' } } }),
      statement({ Principal: '*', Action: 's3:GetObject', Resource: 'arn:aws:s3:::photos/*',
        Condition: { IpAddress: { 'aws:SourceIp': '1.2.3.4' }, NotIpAddress: { 'aws:SourceIp': '5.6.7.8' } } }),
    ]) {
      expect(rulesFromPolicy(policy, bucket)).toBeNull();
    }
  });

  it('tell actions on objects from actions on the bucket', () => {
    expect(isObjectAction('s3:GetObject')).toBe(true);
    expect(isObjectAction('s3:AbortMultipartUpload')).toBe(true);
    expect(isObjectAction('s3:GetBucketObjectLockConfiguration')).toBe(false);
    expect(isObjectAction('s3:ListBucket')).toBe(false);
  });

  it('say what a rule lacks', () => {
    expect(ruleProblem(newRule())).toBe('policyEditor.problemNoOne');
    expect(ruleProblem({ ...newRule(), everyone: true })).toBe('policyEditor.problemNoActions');
    expect(ruleProblem({ ...newRule(), everyone: true, actions: ['s3:ListBucket'], prefix: 'a/' })).toBe('policyEditor.problemPrefixActions');
    expect(ruleProblem({ ...newRule(), everyone: true, allActions: true, addresses: 'from' })).toBe('policyEditor.problemAddresses');
    expect(ruleProblem({ ...newRule(), everyone: true, allActions: true, addresses: 'from', sourceIps: ['10.0.0.0/8'] })).toBeNull();
  });

  it('accept addresses and ranges only', () => {
    expect(isAddressRange('203.0.113.0/24')).toBe(true);
    expect(isAddressRange('203.0.113.5')).toBe(true);
    expect(isAddressRange('2001:db8::/32')).toBe(true);
    expect(isAddressRange('300.1.1.1')).toBe(false);
    expect(isAddressRange('10.0.0.0/33')).toBe(false);
    expect(isAddressRange('office')).toBe(false);
  });
});
