// common.js - Shared configuration and helpers for k6 load tests

import { check, sleep } from 'k6';
import { Rate, Trend, Counter } from 'k6/metrics';
import http from 'k6/http';
import crypto from 'k6/crypto';

// ============================================================================
// Configuration
// ============================================================================

export const config = {
  // Server endpoints
  s3Endpoint: __ENV.S3_ENDPOINT || 'http://localhost:8080',
  consoleEndpoint: __ENV.CONSOLE_ENDPOINT || 'http://localhost:8081',

  // S3 Credentials (create these via web console first!)
  accessKey: __ENV.ACCESS_KEY || 'YOUR_ACCESS_KEY',
  secretKey: __ENV.SECRET_KEY || 'YOUR_SECRET_KEY',

  // Test bucket
  bucket: __ENV.TEST_BUCKET || 'perf-test-bucket',

  // JWT token for console API (optional)
  jwtToken: __ENV.JWT_TOKEN || '',
};

// ============================================================================
// Custom Metrics
// ============================================================================

export const metrics = {
  // Success rates
  uploadSuccessRate: new Rate('upload_success'),
  downloadSuccessRate: new Rate('download_success'),
  listSuccessRate: new Rate('list_success'),
  deleteSuccessRate: new Rate('delete_success'),

  // Latencies
  uploadLatency: new Trend('upload_latency_ms'),
  downloadLatency: new Trend('download_latency_ms'),
  listLatency: new Trend('list_latency_ms'),
  deleteLatency: new Trend('delete_latency_ms'),

  // Throughput
  bytesUploaded: new Counter('bytes_uploaded'),
  bytesDownloaded: new Counter('bytes_downloaded'),
  objectsCreated: new Counter('objects_created'),
  objectsDeleted: new Counter('objects_deleted'),
};

// ============================================================================
// AWS Signature V4 Helper
// ============================================================================

function hmac(key, message) {
  return crypto.hmac('sha256', key, message, 'binary');
}

function getSignatureKey(key, dateStamp, regionName, serviceName) {
  const kDate = hmac('AWS4' + key, dateStamp);
  const kRegion = hmac(kDate, regionName);
  const kService = hmac(kRegion, serviceName);
  const kSigning = hmac(kService, 'aws4_request');
  return kSigning;
}

export function signRequest(method, url, headers, payload) {
  // Parse URL manually (k6 doesn't have URL API)
  const urlMatch = url.match(/^https?:\/\/([^\/]+)(\/[^?]*)?(\?.*)?$/);
  if (!urlMatch) {
    throw new Error(`Invalid URL: ${url}`);
  }

  const host = urlMatch[1];
  const path = urlMatch[2] || '/';
  const queryString = urlMatch[3] ? urlMatch[3].slice(1) : ''; // Remove '?'

  // Canonicalize query string (AWS SigV4 requirement)
  let canonicalQuery = '';
  if (queryString) {
    // Parse query parameters
    const params = [];
    const pairs = queryString.split('&');
    for (let i = 0; i < pairs.length; i++) {
      const pair = pairs[i];
      const eqIndex = pair.indexOf('=');
      if (eqIndex === -1) {
        // Parameter with no value
        params.push({ key: encodeURIComponent(pair), value: '' });
      } else {
        const key = pair.substring(0, eqIndex);
        const value = pair.substring(eqIndex + 1);
        params.push({
          key: encodeURIComponent(key),
          value: encodeURIComponent(value)
        });
      }
    }

    // Sort parameters by key
    params.sort((a, b) => {
      if (a.key < b.key) return -1;
      if (a.key > b.key) return 1;
      return 0;
    });

    // Build canonical query string
    const queryParts = [];
    for (let i = 0; i < params.length; i++) {
      queryParts.push(params[i].key + '=' + params[i].value);
    }
    canonicalQuery = queryParts.join('&');
  }

  // Get current date/time
  const now = new Date();
  const amzDate = now.toISOString().replace(/[:\-]|\.\d{3}/g, '');
  const dateStamp = amzDate.slice(0, 8);

  // AWS credentials
  const accessKey = config.accessKey;
  const secretKey = config.secretKey;
  const region = 'us-east-1';
  const service = 's3';

  // Calculate content hash
  const payloadHash = crypto.sha256(payload || '', 'hex');

  // Create canonical headers
  const canonicalHeaders = `host:${host}\nx-amz-content-sha256:${payloadHash}\nx-amz-date:${amzDate}\n`;
  const signedHeaders = 'host;x-amz-content-sha256;x-amz-date';

  // Create canonical request
  const canonicalRequest = [
    method,
    path,
    canonicalQuery,
    canonicalHeaders,
    signedHeaders,
    payloadHash
  ].join('\n');

  // Create string to sign
  const algorithm = 'AWS4-HMAC-SHA256';
  const credentialScope = `${dateStamp}/${region}/${service}/aws4_request`;
  const stringToSign = [
    algorithm,
    amzDate,
    credentialScope,
    crypto.sha256(canonicalRequest, 'hex')
  ].join('\n');

  // Calculate signature
  const signingKey = getSignatureKey(secretKey, dateStamp, region, service);
  const signature = crypto.hmac('sha256', signingKey, stringToSign, 'hex');

  // Create authorization header
  const authorization = `${algorithm} Credential=${accessKey}/${credentialScope}, SignedHeaders=${signedHeaders}, Signature=${signature}`;

  return {
    'Host': host,
    'Authorization': authorization,
    'x-amz-date': amzDate,
    'x-amz-content-sha256': payloadHash,
  };
}

// ============================================================================
// S3 Operations
// ============================================================================

export function uploadObject(bucket, key, data) {
  const url = `${config.s3Endpoint}/${bucket}/${key}`;
  const headers = signRequest('PUT', url, {}, data);

  const startTime = Date.now();
  const res = http.put(url, data, { headers });
  const duration = Date.now() - startTime;

  // Record metrics
  metrics.uploadLatency.add(duration);
  metrics.uploadSuccessRate.add(res.status === 200 || res.status === 201);

  if (res.status === 200 || res.status === 201) {
    metrics.bytesUploaded.add(data.length);
    metrics.objectsCreated.add(1);
  }

  // Check response
  const success = check(res, {
    'upload status is 200/201': (r) => r.status === 200 || r.status === 201,
  });

  return { success, status: res.status, duration };
}

export function downloadObject(bucket, key) {
  const url = `${config.s3Endpoint}/${bucket}/${key}`;
  const headers = signRequest('GET', url, {}, null);

  const startTime = Date.now();
  const res = http.get(url, { headers });
  const duration = Date.now() - startTime;

  // Record metrics
  metrics.downloadLatency.add(duration);
  metrics.downloadSuccessRate.add(res.status === 200);

  if (res.status === 200) {
    metrics.bytesDownloaded.add(res.body.length);
  }

  // Check response
  const success = check(res, {
    'download status is 200': (r) => r.status === 200,
  });

  return { success, status: res.status, duration, data: res.body };
}

export function listObjects(bucket, prefix = '') {
  const url = `${config.s3Endpoint}/${bucket}?prefix=${prefix}`;
  const headers = signRequest('GET', url, {}, null);

  const startTime = Date.now();
  const res = http.get(url, { headers });
  const duration = Date.now() - startTime;

  // Record metrics
  metrics.listLatency.add(duration);
  metrics.listSuccessRate.add(res.status === 200);

  // Check response
  const success = check(res, {
    'list status is 200': (r) => r.status === 200,
  });

  return { success, status: res.status, duration, body: res.body };
}

export function deleteObject(bucket, key) {
  const url = `${config.s3Endpoint}/${bucket}/${key}`;
  const headers = signRequest('DELETE', url, {}, null);

  const startTime = Date.now();
  const res = http.del(url, null, { headers });
  const duration = Date.now() - startTime;

  // Record metrics
  metrics.deleteLatency.add(duration);
  metrics.deleteSuccessRate.add(res.status === 204 || res.status === 200);

  if (res.status === 204 || res.status === 200) {
    metrics.objectsDeleted.add(1);
  }

  // Check response
  const success = check(res, {
    'delete status is 204/200': (r) => r.status === 204 || r.status === 200,
  });

  return { success, status: res.status, duration };
}

export function createBucket(bucket) {
  const url = `${config.s3Endpoint}/${bucket}`;
  const headers = signRequest('PUT', url, {}, null);

  const res = http.put(url, null, { headers });

  const success = check(res, {
    'create bucket status is 200': (r) => r.status === 200 || r.status === 409, // 409 = already exists (ok)
  });

  return { success, status: res.status };
}

export function deleteBucket(bucket) {
  const url = `${config.s3Endpoint}/${bucket}`;
  const headers = signRequest('DELETE', url, {}, null);

  const res = http.del(url, null, { headers });

  const success = check(res, {
    'delete bucket status is 204': (r) => r.status === 204 || r.status === 200,
  });

  return { success, status: res.status };
}

// ============================================================================
// Console API Operations (requires JWT)
// ============================================================================

export function getPerformanceMetrics() {
  if (!config.jwtToken) {
    console.log('Skipping console API call - no JWT token provided');
    return null;
  }

  const url = `${config.consoleEndpoint}/api/v1/metrics/performance/latencies`;
  const headers = {
    'Authorization': `Bearer ${config.jwtToken}`,
  };

  const res = http.get(url, { headers });

  if (res.status === 200) {
    return JSON.parse(res.body);
  }

  return null;
}

// ============================================================================
// Data Generation Helpers
// ============================================================================

export function randomString(length) {
  const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789';
  let result = '';
  for (let i = 0; i < length; i++) {
    result += chars.charAt(Math.floor(Math.random() * chars.length));
  }
  return result;
}

export function generateData(sizeBytes) {
  // Generate random data of specified size
  const blockSize = 1024; // 1KB blocks
  const blocks = Math.ceil(sizeBytes / blockSize);
  let data = '';

  for (let i = 0; i < blocks; i++) {
    const thisBlockSize = Math.min(blockSize, sizeBytes - (i * blockSize));
    data += randomString(thisBlockSize);
  }

  return data;
}

// ============================================================================
// Test Scenarios
// ============================================================================

export const scenarios = {
  // Ramp-up: gradual increase from 1 to target VUs
  rampUp: (target, duration) => ({
    executor: 'ramping-vus',
    startVUs: 1,
    stages: [
      { duration: duration, target: target },
      { duration: duration, target: target }, // Hold at target
      { duration: '30s', target: 0 },          // Ramp down
    ],
    gracefulRampDown: '30s',
  }),

  // Sustained load: constant VUs for duration
  sustained: (vus, duration) => ({
    executor: 'constant-vus',
    vus: vus,
    duration: duration,
  }),

  // Spike: sudden increase then decrease
  spike: (normalVUs, spikeVUs) => ({
    executor: 'ramping-vus',
    startVUs: normalVUs,
    stages: [
      { duration: '30s', target: normalVUs },  // Normal load
      { duration: '10s', target: spikeVUs },   // Sudden spike
      { duration: '30s', target: spikeVUs },   // Hold spike
      { duration: '10s', target: normalVUs },  // Drop back
      { duration: '30s', target: 0 },          // Ramp down
    ],
    gracefulRampDown: '30s',
  }),

  // Stress: find breaking point
  stress: (maxVUs) => ({
    executor: 'ramping-vus',
    startVUs: 1,
    stages: [
      { duration: '2m', target: 10 },
      { duration: '2m', target: 50 },
      { duration: '2m', target: 100 },
      { duration: '2m', target: 200 },
      { duration: '2m', target: maxVUs },
      { duration: '2m', target: 0 },
    ],
    gracefulRampDown: '30s',
  }),
};

// ============================================================================
// Thresholds (pass/fail criteria)
// ============================================================================

export const defaultThresholds = {
  // Success rates (at least 95% success)
  'upload_success': ['rate>0.95'],
  'download_success': ['rate>0.95'],
  'list_success': ['rate>0.95'],
  'delete_success': ['rate>0.95'],

  // Latencies (p95 under specified limits)
  'upload_latency_ms': ['p(95)<1000'],      // 95% under 1s
  'download_latency_ms': ['p(95)<500'],     // 95% under 500ms
  'list_latency_ms': ['p(95)<200'],         // 95% under 200ms
  'delete_latency_ms': ['p(95)<100'],       // 95% under 100ms

  // HTTP duration (overall request time)
  'http_req_duration': ['p(95)<2000'],      // 95% under 2s
  'http_req_failed': ['rate<0.05'],         // Less than 5% errors
};

// ============================================================================
// Setup & Teardown Helpers
// ============================================================================

export function setupTest() {
  console.log('=================================================');
  console.log('MaxIOFS Performance Test');
  console.log('=================================================');
  console.log(`S3 Endpoint:     ${config.s3Endpoint}`);
  console.log(`Console Endpoint: ${config.consoleEndpoint}`);
  console.log(`Test Bucket:     ${config.bucket}`);
  console.log(`Access Key:      ${config.accessKey.substring(0, 8)}...`);
  console.log('=================================================');

  // Create test bucket
  console.log(`Creating test bucket: ${config.bucket}`);
  const result = createBucket(config.bucket);

  if (result.success) {
    console.log('✓ Test bucket ready');
  } else {
    console.error(`✗ Failed to create bucket: ${result.status}`);
  }

  return { bucket: config.bucket };
}

export function teardownTest() {
  console.log('=================================================');
  console.log('Test completed - cleanup not performed');
  console.log(`To manually clean up, delete bucket: ${config.bucket}`);
  console.log('=================================================');
}

// ============================================================================
// Export all
// ============================================================================

export default {
  config,
  metrics,
  signRequest,
  uploadObject,
  downloadObject,
  listObjects,
  deleteObject,
  createBucket,
  deleteBucket,
  getPerformanceMetrics,
  randomString,
  generateData,
  scenarios,
  defaultThresholds,
  setupTest,
  teardownTest,
};
