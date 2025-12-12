// upload_test.js - k6 load test for upload performance
//
// This test validates upload performance under various load conditions.
// Tests small, medium, and large file uploads with realistic patterns.
//
// Usage:
//   k6 run upload_test.js
//   k6 run --env S3_ENDPOINT=http://server:8080 upload_test.js
//   k6 run --vus 50 --duration 5m upload_test.js

import { sleep } from 'k6';
import { SharedArray } from 'k6/data';
import {
  config,
  metrics,
  uploadObject,
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
  // Test scenario: ramp-up from 1 to 50 VUs over 2 minutes
  scenarios: {
    upload_rampup: scenarios.rampUp(50, '2m'),
  },

  // Performance thresholds (test fails if not met)
  thresholds: {
    ...defaultThresholds,

    // Upload-specific thresholds
    'upload_success': ['rate>0.95'],           // 95% success rate
    'upload_latency_ms': ['p(95)<2000'],       // p95 under 2s
    'upload_latency_ms': ['p(99)<5000'],       // p99 under 5s
    'bytes_uploaded': ['count>1000000'],       // At least 1MB uploaded
    'objects_created': ['count>100'],          // At least 100 objects
  },

  // Output results to JSON
  summaryTrendStats: ['min', 'med', 'avg', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

// ============================================================================
// Test Data - File Sizes
// ============================================================================

// Define realistic file size distribution
const fileSizes = new SharedArray('file_sizes', function () {
  return [
    { size: 1024, weight: 50, name: '1KB' },           // 50% small files
    { size: 10240, weight: 30, name: '10KB' },         // 30% medium files
    { size: 102400, weight: 15, name: '100KB' },       // 15% large files
    { size: 1048576, weight: 4, name: '1MB' },         // 4% very large
    { size: 5242880, weight: 1, name: '5MB' },         // 1% huge files
  ];
});

// ============================================================================
// Setup & Teardown
// ============================================================================

export function setup() {
  console.log('=====================================');
  console.log('MaxIOFS Upload Performance Test');
  console.log('=====================================');
  console.log(`S3 Endpoint:  ${config.s3Endpoint}`);
  console.log(`Test Bucket:  ${config.bucket}`);
  console.log(`Access Key:   ${config.accessKey.substring(0, 8)}...`);
  console.log('=====================================');

  return setupTest();
}

export function teardown(data) {
  console.log('=====================================');
  console.log('Upload Test Summary');
  console.log('=====================================');
  console.log('Test completed successfully');
  console.log('Note: Objects remain in bucket for inspection');
  console.log(`To clean up: aws s3 rb s3://${config.bucket} --force`);
  console.log('=====================================');

  teardownTest();
}

// ============================================================================
// Helper Functions
// ============================================================================

// Select file size based on weighted distribution
function selectFileSize() {
  const totalWeight = fileSizes.reduce((sum, item) => sum + item.weight, 0);
  let random = Math.random() * totalWeight;

  for (let item of fileSizes) {
    random -= item.weight;
    if (random <= 0) {
      return item;
    }
  }

  return fileSizes[0]; // Fallback
}

// Generate unique object key
function generateObjectKey(vu, iter, sizeInfo) {
  const timestamp = Date.now();
  const random = randomString(8);
  return `upload-test/vu${vu}/iter${iter}/${sizeInfo.name}-${timestamp}-${random}.bin`;
}

// ============================================================================
// Test Scenario
// ============================================================================

export default function (data) {
  const vu = __VU;
  const iter = __ITER;

  // Select file size (weighted random)
  const sizeInfo = selectFileSize();

  // Generate object key
  const key = generateObjectKey(vu, iter, sizeInfo);

  // Generate test data
  const objectData = generateData(sizeInfo.size);

  // Upload object
  const uploadResult = uploadObject(config.bucket, key, objectData);

  if (!uploadResult.success) {
    console.error(`[VU ${vu}] Upload failed: ${key} (${uploadResult.status})`);
  } else {
    // Log successful large uploads
    if (sizeInfo.size >= 1048576) {
      console.log(`[VU ${vu}] Uploaded ${sizeInfo.name}: ${key} (${uploadResult.duration}ms)`);
    }
  }

  // Think time: simulate realistic client behavior
  // Smaller files = faster user workflow
  const thinkTime = sizeInfo.size < 10240 ? 0.5 : 1.0;
  sleep(thinkTime);

  // Optional: cleanup some objects to avoid filling disk
  // Delete 10% of small files to maintain steady state
  if (sizeInfo.size <= 10240 && Math.random() < 0.1) {
    deleteObject(config.bucket, key);
  }
}

// ============================================================================
// Custom Metrics Summary (printed at end)
// ============================================================================

export function handleSummary(data) {
  const uploadSuccess = data.metrics.upload_success;
  const uploadLatency = data.metrics.upload_latency_ms;
  const bytesUploaded = data.metrics.bytes_uploaded;
  const objectsCreated = data.metrics.objects_created;

  console.log('\n=====================================');
  console.log('Upload Performance Results');
  console.log('=====================================');

  if (uploadSuccess) {
    console.log(`Success Rate:     ${(uploadSuccess.values.rate * 100).toFixed(2)}%`);
  }

  if (uploadLatency) {
    console.log(`Latency (p50):    ${uploadLatency.values.med.toFixed(2)} ms`);
    console.log(`Latency (p95):    ${uploadLatency.values['p(95)'].toFixed(2)} ms`);
    console.log(`Latency (p99):    ${uploadLatency.values['p(99)'].toFixed(2)} ms`);
    console.log(`Latency (max):    ${uploadLatency.values.max.toFixed(2)} ms`);
  }

  if (bytesUploaded) {
    const mb = bytesUploaded.values.count / (1024 * 1024);
    console.log(`Data Uploaded:    ${mb.toFixed(2)} MB`);
  }

  if (objectsCreated) {
    console.log(`Objects Created:  ${objectsCreated.values.count}`);
  }

  console.log('=====================================\n');

  // Return default summary
  return {
    'stdout': '', // Suppress default stdout (we printed custom)
    'upload_test_summary.json': JSON.stringify(data, null, 2),
  };
}
