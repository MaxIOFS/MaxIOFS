// mixed_workload.js - k6 load test for realistic mixed S3 operations
//
// This test simulates realistic production workload with:
// - 50% downloads (most common operation)
// - 30% uploads (second most common)
// - 15% list operations
// - 5% deletes
//
// Usage:
//   k6 run mixed_workload.js
//   k6 run --env S3_ENDPOINT=http://server:8080 mixed_workload.js
//   k6 run --vus 75 --duration 5m mixed_workload.js

import { sleep } from 'k6';
import { SharedArray } from 'k6/data';
import {
  config,
  metrics,
  uploadObject,
  downloadObject,
  listObjects,
  deleteObject,
  generateData,
  randomString,
  scenarios,
  defaultThresholds,
  setupTest,
  teardownTest,
} from './common.js';

// ============================================================================
// Test Configuration
// ============================================================================

export const options = {
  // Test scenario: spike test to validate resilience
  scenarios: {
    mixed_spike: scenarios.spike(25, 100),
  },

  // Performance thresholds
  thresholds: {
    ...defaultThresholds,

    // Mixed workload thresholds (more lenient than single-operation tests)
    'upload_success': ['rate>0.90'],
    'download_success': ['rate>0.95'],
    'list_success': ['rate>0.98'],
    'delete_success': ['rate>0.90'],

    'upload_latency_ms': ['p(95)<3000'],
    'download_latency_ms': ['p(95)<1000'],
    'list_latency_ms': ['p(95)<500'],
    'delete_latency_ms': ['p(95)<200'],

    'http_req_duration': ['p(95)<3000'],
    'http_req_failed': ['rate<0.10'],           // 10% error budget for mixed workload
  },

  summaryTrendStats: ['min', 'med', 'avg', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

// ============================================================================
// Workload Distribution
// ============================================================================

const operations = [
  { name: 'download', weight: 50 },
  { name: 'upload', weight: 30 },
  { name: 'list', weight: 15 },
  { name: 'delete', weight: 5 },
];

// File size distribution (weighted)
const fileSizes = [
  { size: 1024, weight: 40, name: '1KB' },
  { size: 10240, weight: 30, name: '10KB' },
  { size: 51200, weight: 20, name: '50KB' },
  { size: 102400, weight: 7, name: '100KB' },
  { size: 524288, weight: 2, name: '512KB' },
  { size: 1048576, weight: 1, name: '1MB' },
];

// Convert to SharedArray for efficient sharing across VUs
const operationWeights = new SharedArray('operations', function () {
  return operations;
});

const sizeWeights = new SharedArray('file_sizes', function () {
  return fileSizes;
});

// ============================================================================
// State Management (per VU)
// ============================================================================

// Track uploaded objects per VU for download/delete operations
let uploadedObjects = [];
const MAX_TRACKED_OBJECTS = 50; // Limit memory usage

// ============================================================================
// Setup & Teardown
// ============================================================================

export function setup() {
  console.log('=====================================');
  console.log('MaxIOFS Mixed Workload Test');
  console.log('=====================================');
  console.log('Workload Distribution:');
  console.log('  Downloads:  50%');
  console.log('  Uploads:    30%');
  console.log('  Lists:      15%');
  console.log('  Deletes:     5%');
  console.log('=====================================');
  console.log(`S3 Endpoint:  ${config.s3Endpoint}`);
  console.log(`Test Bucket:  ${config.bucket}`);
  console.log('=====================================\n');

  const data = setupTest();

  console.log('Uploading seed objects for download tests...');

  // Upload seed objects for download operations
  const seedObjects = [];
  for (let i = 0; i < 20; i++) {
    const key = `seed/object-${i}-${randomString(8)}.bin`;
    const size = 10240; // 10KB seed objects
    const objectData = generateData(size);

    const result = uploadObject(config.bucket, key, objectData);

    if (result.success) {
      seedObjects.push(key);
      if (i % 5 === 0) {
        console.log(`✓ Uploaded seed object ${i + 1}/20`);
      }
    }

    sleep(0.05);
  }

  console.log(`\nSetup complete: ${seedObjects.length} seed objects ready`);
  console.log('Starting mixed workload test...\n');

  return {
    ...data,
    seedObjects: seedObjects,
  };
}

export function teardown(data) {
  console.log('\n=====================================');
  console.log('Mixed Workload Test Summary');
  console.log('=====================================');
  console.log('Test completed successfully');
  console.log(`Seed objects: ${data.seedObjects.length}`);
  console.log('=====================================');

  teardownTest();
}

// ============================================================================
// Helper Functions
// ============================================================================

// Select operation based on weighted distribution
function selectOperation() {
  const totalWeight = operationWeights.reduce((sum, op) => sum + op.weight, 0);
  let random = Math.random() * totalWeight;

  for (let op of operationWeights) {
    random -= op.weight;
    if (random <= 0) {
      return op.name;
    }
  }

  return 'download'; // Fallback
}

// Select file size based on weighted distribution
function selectFileSize() {
  const totalWeight = sizeWeights.reduce((sum, item) => sum + item.weight, 0);
  let random = Math.random() * totalWeight;

  for (let item of sizeWeights) {
    random -= item.weight;
    if (random <= 0) {
      return item;
    }
  }

  return sizeWeights[0]; // Fallback
}

// Generate object key
function generateKey(vu, iter, prefix = 'mixed') {
  const timestamp = Date.now();
  const random = randomString(6);
  return `${prefix}/vu${vu}/iter${iter}-${timestamp}-${random}.bin`;
}

// ============================================================================
// Operation Handlers
// ============================================================================

function handleUpload(vu, iter, data) {
  const sizeInfo = selectFileSize();
  const key = generateKey(vu, iter, 'uploads');
  const objectData = generateData(sizeInfo.size);

  const result = uploadObject(config.bucket, key, objectData);

  if (result.success) {
    // Track uploaded object for future downloads/deletes
    uploadedObjects.push({ key: key, size: sizeInfo.size });

    // Limit tracked objects to prevent memory issues
    if (uploadedObjects.length > MAX_TRACKED_OBJECTS) {
      uploadedObjects.shift(); // Remove oldest
    }

    // Log large uploads
    if (sizeInfo.size >= 524288) {
      console.log(`[VU ${vu}] Uploaded ${sizeInfo.name}: ${key}`);
    }
  }

  return result;
}

function handleDownload(vu, iter, data) {
  let key;

  // 70% chance: download from our uploaded objects
  // 30% chance: download from seed objects
  if (uploadedObjects.length > 0 && Math.random() < 0.7) {
    const randomIndex = Math.floor(Math.random() * uploadedObjects.length);
    key = uploadedObjects[randomIndex].key;
  } else if (data.seedObjects && data.seedObjects.length > 0) {
    const randomIndex = Math.floor(Math.random() * data.seedObjects.length);
    key = data.seedObjects[randomIndex];
  } else {
    // No objects to download - skip
    return { success: true, status: 200, duration: 0 };
  }

  const result = downloadObject(config.bucket, key);

  if (!result.success && result.status === 404) {
    // Object may have been deleted - not a critical error
    // Remove from tracking
    uploadedObjects = uploadedObjects.filter(obj => obj.key !== key);
  }

  return result;
}

function handleList(vu, iter, data) {
  // List objects with various prefixes
  const prefixes = ['uploads/', 'seed/', 'mixed/', ''];
  const prefix = prefixes[Math.floor(Math.random() * prefixes.length)];

  const result = listObjects(config.bucket, prefix);

  return result;
}

function handleDelete(vu, iter, data) {
  // Only delete from our uploaded objects (don't delete seed objects)
  if (uploadedObjects.length === 0) {
    // Nothing to delete - skip
    return { success: true, status: 204, duration: 0 };
  }

  // Delete oldest object (FIFO)
  const objToDelete = uploadedObjects.shift();
  const result = deleteObject(config.bucket, objToDelete.key);

  return result;
}

// ============================================================================
// Test Scenario
// ============================================================================

export default function (data) {
  const vu = __VU;
  const iter = __ITER;

  // Select operation based on distribution
  const operation = selectOperation();

  let result;
  switch (operation) {
    case 'upload':
      result = handleUpload(vu, iter, data);
      sleep(0.5); // Upload think time
      break;

    case 'download':
      result = handleDownload(vu, iter, data);
      sleep(0.2); // Download think time (faster)
      break;

    case 'list':
      result = handleList(vu, iter, data);
      sleep(0.3); // List think time
      break;

    case 'delete':
      result = handleDelete(vu, iter, data);
      sleep(0.1); // Delete think time (fastest)
      break;

    default:
      console.error(`[VU ${vu}] Unknown operation: ${operation}`);
      sleep(1);
  }

  // Log errors
  if (result && !result.success) {
    if (iter % 10 === 0) {
      console.error(
        `[VU ${vu}] ${operation} failed (${result.status}) - ` +
        `tracked objects: ${uploadedObjects.length}`
      );
    }
  }
}

// ============================================================================
// Custom Metrics Summary
// ============================================================================

export function handleSummary(data) {
  console.log('\n=====================================');
  console.log('Mixed Workload Performance Results');
  console.log('=====================================');

  // Operation-specific metrics
  const operations = [
    { name: 'Upload', metric: 'upload_success', latency: 'upload_latency_ms' },
    { name: 'Download', metric: 'download_success', latency: 'download_latency_ms' },
    { name: 'List', metric: 'list_success', latency: 'list_latency_ms' },
    { name: 'Delete', metric: 'delete_success', latency: 'delete_latency_ms' },
  ];

  for (let op of operations) {
    const successMetric = data.metrics[op.metric];
    const latencyMetric = data.metrics[op.latency];

    if (successMetric && successMetric.values.rate > 0) {
      console.log(`\n${op.name}:`);
      console.log(`  Success Rate: ${(successMetric.values.rate * 100).toFixed(2)}%`);

      if (latencyMetric) {
        console.log(`  p50: ${latencyMetric.values.med.toFixed(2)} ms`);
        console.log(`  p95: ${latencyMetric.values['p(95)'].toFixed(2)} ms`);
        console.log(`  p99: ${latencyMetric.values['p(99)'].toFixed(2)} ms`);
      }
    }
  }

  // Overall metrics
  console.log('\nOverall:');

  if (data.metrics.http_reqs) {
    const requests = data.metrics.http_reqs.values.count;
    const duration = data.state.testRunDurationMs / 1000;
    console.log(`  Total Requests:   ${requests}`);
    console.log(`  Requests/sec:     ${(requests / duration).toFixed(2)}`);
  }

  if (data.metrics.bytes_uploaded) {
    const mb = data.metrics.bytes_uploaded.values.count / (1024 * 1024);
    console.log(`  Data Uploaded:    ${mb.toFixed(2)} MB`);
  }

  if (data.metrics.bytes_downloaded) {
    const mb = data.metrics.bytes_downloaded.values.count / (1024 * 1024);
    console.log(`  Data Downloaded:  ${mb.toFixed(2)} MB`);
  }

  if (data.metrics.objects_created) {
    console.log(`  Objects Created:  ${data.metrics.objects_created.values.count}`);
  }

  if (data.metrics.objects_deleted) {
    console.log(`  Objects Deleted:  ${data.metrics.objects_deleted.values.count}`);
  }

  console.log('=====================================\n');

  // Return summary
  return {
    'stdout': '',
    'mixed_workload_summary.json': JSON.stringify(data, null, 2),
  };
}
