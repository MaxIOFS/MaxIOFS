// download_test.js - k6 load test for download performance
//
// This test validates download performance under various load conditions.
// Tests concurrent downloads, cache behavior, and read throughput.
//
// Usage:
//   k6 run download_test.js
//   k6 run --env S3_ENDPOINT=http://server:8080 download_test.js
//   k6 run --vus 100 --duration 3m download_test.js

import { sleep } from 'k6';
import { SharedArray } from 'k6/data';
import {
  config,
  metrics,
  uploadObject,
  downloadObject,
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
  // Test scenario: sustained load with 100 concurrent VUs
  scenarios: {
    download_sustained: scenarios.sustained(100, '3m'),
  },

  // Performance thresholds (test fails if not met)
  thresholds: {
    ...defaultThresholds,

    // Download-specific thresholds
    'download_success': ['rate>0.98'],         // 98% success (higher than uploads)
    'download_latency_ms': ['p(95)<500'],      // p95 under 500ms (faster than uploads)
    'download_latency_ms': ['p(99)<1000'],     // p99 under 1s
    'bytes_downloaded': ['count>10000000'],    // At least 10MB downloaded
  },

  // Output results
  summaryTrendStats: ['min', 'med', 'avg', 'p(90)', 'p(95)', 'p(99)', 'max'],
};

// ============================================================================
// Test Data - Pre-populated Objects
// ============================================================================

// Objects to create during setup
const testObjects = [
  { key: 'download-test/tiny.bin', size: 512 },           // 512 bytes
  { key: 'download-test/small-1.bin', size: 1024 },       // 1KB
  { key: 'download-test/small-2.bin', size: 2048 },       // 2KB
  { key: 'download-test/small-3.bin', size: 4096 },       // 4KB
  { key: 'download-test/medium-1.bin', size: 10240 },     // 10KB
  { key: 'download-test/medium-2.bin', size: 51200 },     // 50KB
  { key: 'download-test/medium-3.bin', size: 102400 },    // 100KB
  { key: 'download-test/large-1.bin', size: 512000 },     // 500KB
  { key: 'download-test/large-2.bin', size: 1048576 },    // 1MB
  { key: 'download-test/large-3.bin', size: 2097152 },    // 2MB
  { key: 'download-test/huge.bin', size: 5242880 },       // 5MB
];

// Shared array for VUs to access
const objectKeys = new SharedArray('object_keys', function () {
  return testObjects.map(obj => ({
    key: obj.key,
    size: obj.size,
    // Cache hotness: smaller files accessed more frequently
    weight: obj.size < 10240 ? 50 : obj.size < 102400 ? 30 : 20,
  }));
});

// ============================================================================
// Setup & Teardown
// ============================================================================

export function setup() {
  console.log('=====================================');
  console.log('MaxIOFS Download Performance Test');
  console.log('=====================================');
  console.log(`S3 Endpoint:  ${config.s3Endpoint}`);
  console.log(`Test Bucket:  ${config.bucket}`);
  console.log('=====================================');

  const data = setupTest();

  console.log('\nUploading test objects...');

  // Upload all test objects
  let successCount = 0;
  for (let obj of testObjects) {
    const objectData = generateData(obj.size);
    const result = uploadObject(config.bucket, obj.key, objectData);

    if (result.success) {
      successCount++;
      console.log(`✓ Uploaded ${obj.key} (${obj.size} bytes)`);
    } else {
      console.error(`✗ Failed to upload ${obj.key}: ${result.status}`);
    }

    // Small delay to avoid overwhelming server during setup
    sleep(0.1);
  }

  console.log(`\nSetup complete: ${successCount}/${testObjects.length} objects ready`);
  console.log('Starting download test...\n');

  return {
    ...data,
    objectCount: successCount,
  };
}

export function teardown(data) {
  console.log('\n=====================================');
  console.log('Download Test Summary');
  console.log('=====================================');
  console.log(`Test completed successfully`);
  console.log(`Objects in bucket: ${data.objectCount}`);
  console.log('Note: Test objects remain for inspection');
  console.log('=====================================');

  teardownTest();
}

// ============================================================================
// Helper Functions
// ============================================================================

// Select object key based on weighted distribution (simulates cache hot/cold)
function selectObjectKey() {
  const totalWeight = objectKeys.reduce((sum, item) => sum + item.weight, 0);
  let random = Math.random() * totalWeight;

  for (let item of objectKeys) {
    random -= item.weight;
    if (random <= 0) {
      return item;
    }
  }

  return objectKeys[0]; // Fallback
}

// ============================================================================
// Test Scenario
// ============================================================================

export default function (data) {
  const vu = __VU;
  const iter = __ITER;

  // Select object to download (weighted random - simulates cache behavior)
  const objInfo = selectObjectKey();

  // Download object
  const downloadResult = downloadObject(config.bucket, objInfo.key);

  if (!downloadResult.success) {
    console.error(`[VU ${vu}] Download failed: ${objInfo.key} (${downloadResult.status})`);
  } else {
    // Verify downloaded data size
    if (downloadResult.data && downloadResult.data.length !== objInfo.size) {
      console.warn(
        `[VU ${vu}] Size mismatch: ${objInfo.key} ` +
        `(expected ${objInfo.size}, got ${downloadResult.data.length})`
      );
    }

    // Log large file downloads
    if (objInfo.size >= 1048576 && iter % 10 === 0) {
      console.log(
        `[VU ${vu}] Downloaded ${objInfo.key} ` +
        `(${objInfo.size} bytes, ${downloadResult.duration}ms)`
      );
    }
  }

  // Think time: simulate realistic client behavior
  // Smaller files = faster requests (cache hits)
  // Larger files = more processing time
  const thinkTime = objInfo.size < 10240 ? 0.1 : objInfo.size < 102400 ? 0.3 : 0.5;
  sleep(thinkTime);

  // Occasionally test range requests (partial downloads)
  // This tests HTTP Range header support
  if (iter % 20 === 0 && objInfo.size > 10240) {
    // TODO: Add range request test when implemented in common.js
    // For now, just do a regular download
  }
}

// ============================================================================
// Custom Metrics Summary
// ============================================================================

export function handleSummary(data) {
  const downloadSuccess = data.metrics.download_success;
  const downloadLatency = data.metrics.download_latency_ms;
  const bytesDownloaded = data.metrics.bytes_downloaded;

  console.log('\n=====================================');
  console.log('Download Performance Results');
  console.log('=====================================');

  if (downloadSuccess) {
    console.log(`Success Rate:     ${(downloadSuccess.values.rate * 100).toFixed(2)}%`);
  }

  if (downloadLatency) {
    console.log(`Latency (min):    ${downloadLatency.values.min.toFixed(2)} ms`);
    console.log(`Latency (p50):    ${downloadLatency.values.med.toFixed(2)} ms`);
    console.log(`Latency (p95):    ${downloadLatency.values['p(95)'].toFixed(2)} ms`);
    console.log(`Latency (p99):    ${downloadLatency.values['p(99)'].toFixed(2)} ms`);
    console.log(`Latency (max):    ${downloadLatency.values.max.toFixed(2)} ms`);
  }

  if (bytesDownloaded) {
    const mb = bytesDownloaded.values.count / (1024 * 1024);
    console.log(`Data Downloaded:  ${mb.toFixed(2)} MB`);
  }

  if (data.metrics.http_reqs) {
    const requests = data.metrics.http_reqs.values.count;
    const duration = data.state.testRunDurationMs / 1000;
    const rps = requests / duration;
    console.log(`Requests/sec:     ${rps.toFixed(2)}`);

    if (bytesDownloaded) {
      const mbps = (bytesDownloaded.values.count / (1024 * 1024)) / duration;
      console.log(`Throughput:       ${mbps.toFixed(2)} MB/s`);
    }
  }

  console.log('=====================================\n');

  // Check cache effectiveness (hot objects should be faster)
  if (downloadLatency) {
    const p50 = downloadLatency.values.med;
    const p95 = downloadLatency.values['p(95)'];
    const ratio = p95 / p50;

    console.log('Cache Analysis:');
    console.log(`p50/p95 ratio:    ${ratio.toFixed(2)}x`);

    if (ratio < 2.0) {
      console.log('✓ Good cache hit rate (consistent latency)');
    } else if (ratio < 5.0) {
      console.log('⚠ Moderate cache effectiveness');
    } else {
      console.log('✗ Poor cache effectiveness (high variance)');
    }
    console.log('=====================================\n');
  }

  // Return summary
  return {
    'stdout': '',
    'download_test_summary.json': JSON.stringify(data, null, 2),
  };
}
