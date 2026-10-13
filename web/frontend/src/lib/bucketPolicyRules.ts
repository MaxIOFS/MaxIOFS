// A bucket policy as the console edits it: a list of rules, each saying who,
// what, where and when. A rule is one statement of the policy document.

export type RuleEffect = 'Allow' | 'Deny';
export type RuleAddresses = 'any' | 'from' | 'notFrom';
export type RuleTransport = 'any' | 'secure' | 'insecure';

export interface PolicyRule {
  effect: RuleEffect;
  // Everyone, including requests without credentials; otherwise the users named.
  everyone: boolean;
  // Users as the policy names them: user ARNs (arn:aws:iam:::user/<name>) or IDs.
  users: string[];
  // Every S3 action, or the actions listed.
  allActions: boolean;
  actions: string[];
  // Objects under this prefix only; empty for the whole bucket.
  prefix: string;
  addresses: RuleAddresses;
  sourceIps: string[];
  transport: RuleTransport;
}

interface Statement {
  Effect: string;
  Principal?: unknown;
  Action?: unknown;
  Resource?: unknown;
  Condition?: Record<string, Record<string, unknown>>;
  [element: string]: unknown;
}

export interface BucketPolicyDocument {
  Version?: string;
  Statement?: Statement | Statement[];
}

const USER_ARN_PREFIX = 'arn:aws:iam:::user/';

export function userARN(username: string): string {
  return USER_ARN_PREFIX + username;
}

// The name a principal is shown with: the user name of a user ARN, or the ID.
export function principalName(principal: string): string {
  return principal.startsWith(USER_ARN_PREFIX) ? principal.slice(USER_ARN_PREFIX.length) : principal;
}

// Actions on objects apply to <bucket>/<key>; the others to the bucket.
const OBJECT_ACTIONS_WITHOUT_OBJECT = new Set(['s3:AbortMultipartUpload', 's3:ListMultipartUploadParts']);

export function isObjectAction(action: string): boolean {
  return (action.includes('Object') && !action.includes('Bucket')) || OBJECT_ACTIONS_WITHOUT_OBJECT.has(action);
}

// A bucket policy does not decide who manages the policy itself.
export const POLICY_MANAGEMENT_ACTIONS = new Set(['s3:GetBucketPolicy', 's3:PutBucketPolicy', 's3:DeleteBucketPolicy']);

export function newRule(): PolicyRule {
  return {
    effect: 'Allow',
    everyone: false,
    users: [],
    allActions: false,
    actions: [],
    prefix: '',
    addresses: 'any',
    sourceIps: [],
    transport: 'any',
  };
}

function strings(value: unknown): string[] | null {
  if (typeof value === 'string') return [value];
  if (Array.isArray(value) && value.every((v) => typeof v === 'string')) return value as string[];
  return null;
}

// The resources a rule's actions apply to.
export function ruleResources(rule: PolicyRule, bucket: string): string[] {
  const bucketARN = 'arn:aws:s3:::' + bucket;
  if (rule.prefix) return [bucketARN + '/' + rule.prefix + '*'];
  if (rule.allActions) return [bucketARN, bucketARN + '/*'];
  const resources: string[] = [];
  if (rule.actions.some((a) => !isObjectAction(a))) resources.push(bucketARN);
  if (rule.actions.some(isObjectAction)) resources.push(bucketARN + '/*');
  return resources;
}

function ruleFromStatement(st: Statement, bucket: string): PolicyRule | null {
  for (const element of Object.keys(st)) {
    if (!['Sid', 'Effect', 'Principal', 'Action', 'Resource', 'Condition'].includes(element)) return null;
  }
  if (st.Effect !== 'Allow' && st.Effect !== 'Deny') return null;
  const rule = newRule();
  rule.effect = st.Effect;

  if (st.Principal === '*') {
    rule.everyone = true;
  } else if (st.Principal && typeof st.Principal === 'object' && !Array.isArray(st.Principal)) {
    const principal = st.Principal as Record<string, unknown>;
    if (Object.keys(principal).some((k) => k !== 'AWS')) return null;
    const names = strings(principal.AWS);
    if (!names || names.length === 0) return null;
    if (names.includes('*')) rule.everyone = true;
    else rule.users = names;
  } else {
    return null;
  }

  const actions = strings(st.Action);
  if (!actions || actions.length === 0) return null;
  if (actions.includes('*') || actions.includes('s3:*')) rule.allActions = true;
  else if (actions.some((a) => a.includes('*'))) return null;
  else rule.actions = actions;

  const resources = strings(st.Resource);
  if (!resources || resources.length === 0) return null;
  const bucketARN = 'arn:aws:s3:::' + bucket;
  const objects = bucketARN + '/*';
  if (resources.length === 1 && resources[0].startsWith(bucketARN + '/') && resources[0] !== objects) {
    // Objects under a prefix: <bucket>/<prefix>*, object actions only.
    const pattern = resources[0].slice(bucketARN.length + 1);
    if (!pattern.endsWith('*') || pattern.slice(0, -1).includes('*') || rule.allActions) return null;
    if (!rule.actions.every(isObjectAction)) return null;
    rule.prefix = pattern.slice(0, -1);
  } else {
    // The whole bucket: the resources must include those the actions apply
    // to; the other of the two matches none of them. Every action needs both.
    if (resources.some((r) => r !== bucketARN && r !== objects)) return null;
    if (ruleResources(rule, bucket).some((r) => !resources.includes(r))) return null;
  }

  for (const [operator, keys] of Object.entries(st.Condition || {})) {
    for (const [key, value] of Object.entries(keys || {})) {
      const values = strings(value);
      if (!values || values.length === 0) return null;
      if (key.toLowerCase() === 'aws:sourceip' && (operator === 'IpAddress' || operator === 'NotIpAddress')) {
        if (rule.addresses !== 'any') return null;
        rule.addresses = operator === 'IpAddress' ? 'from' : 'notFrom';
        rule.sourceIps = values;
      } else if (key.toLowerCase() === 'aws:securetransport' && operator === 'Bool' && values.length === 1) {
        if (rule.transport !== 'any') return null;
        rule.transport = values[0].toLowerCase() === 'true' ? 'secure' : 'insecure';
      } else {
        return null;
      }
    }
  }
  return rule;
}

// The rules of a policy, or null when the policy says something the editor
// cannot show.
export function rulesFromPolicy(policy: BucketPolicyDocument | null | undefined, bucket: string): PolicyRule[] | null {
  if (!policy) return [];
  if (!policy.Statement) return null;
  const statements = Array.isArray(policy.Statement) ? policy.Statement : [policy.Statement];
  const rules: PolicyRule[] = [];
  for (const st of statements) {
    const rule = ruleFromStatement(st, bucket);
    if (!rule) return null;
    rules.push(rule);
  }
  return rules;
}

// The policy document of a list of rules.
export function policyFromRules(rules: PolicyRule[], bucket: string): BucketPolicyDocument {
  return {
    Version: '2012-10-17',
    Statement: rules.map((rule) => {
      const statement: Statement = {
        Effect: rule.effect,
        Principal: rule.everyone ? '*' : { AWS: rule.users.length === 1 ? rule.users[0] : rule.users },
        Action: rule.allActions ? 's3:*' : rule.actions.length === 1 ? rule.actions[0] : rule.actions,
        Resource: (() => {
          const resources = ruleResources(rule, bucket);
          return resources.length === 1 ? resources[0] : resources;
        })(),
      };
      const condition: Record<string, Record<string, unknown>> = {};
      if (rule.addresses !== 'any') {
        condition[rule.addresses === 'from' ? 'IpAddress' : 'NotIpAddress'] = {
          'aws:SourceIp': rule.sourceIps.length === 1 ? rule.sourceIps[0] : rule.sourceIps,
        };
      }
      if (rule.transport !== 'any') {
        condition.Bool = { 'aws:SecureTransport': rule.transport === 'secure' ? 'true' : 'false' };
      }
      if (Object.keys(condition).length > 0) statement.Condition = condition;
      return statement;
    }),
  };
}

// An address or range aws:SourceIp accepts.
export function isAddressRange(value: string): boolean {
  const [address, bits, extra] = value.trim().split('/');
  if (extra !== undefined || !address) return false;
  const ipv4 = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/.test(address);
  const ipv6 = !ipv4 && address.includes(':') && /^[0-9a-fA-F:.]+$/.test(address);
  if (!ipv4 && !ipv6) return false;
  if (bits === undefined) return true;
  const n = Number(bits);
  return /^\d+$/.test(bits) && n >= 0 && n <= (ipv4 ? 32 : 128);
}

// What a rule still lacks before it can be saved, as a translation key.
export function ruleProblem(rule: PolicyRule): string | null {
  if (!rule.everyone && rule.users.length === 0) return 'policyEditor.problemNoOne';
  if (!rule.allActions && rule.actions.length === 0) return 'policyEditor.problemNoActions';
  if (rule.prefix && (rule.allActions || rule.actions.some((a) => !isObjectAction(a)))) return 'policyEditor.problemPrefixActions';
  if (rule.addresses !== 'any' && (rule.sourceIps.length === 0 || !rule.sourceIps.every(isAddressRange))) {
    return 'policyEditor.problemAddresses';
  }
  return null;
}
