#!/bin/bash
# MaxIOFS Performance Testing Script for Linux
# Run this on a production-like Linux environment for accurate benchmarks
#
# Prerequisites:
# - k6 installed (https://k6.io/docs/get-started/installation/)
# - MaxIOFS server running on localhost:8080 (S3 API)
# - MaxIOFS console running on localhost:8081 (Admin API)
# - AWS CLI configured with credentials (or use the credentials below)

set -e  # Exit on error

echo "======================================"
echo "MaxIOFS Performance Testing - Linux"
echo "======================================"
echo ""

# Configuration
export S3_ENDPOINT="http://localhost:8080"
export CONSOLE_ENDPOINT="http://localhost:8081"
export TEST_BUCKET="perf-test-bucket"

# Replace these with your actual credentials
# You can get these from the MaxIOFS console or use existing AWS CLI credentials
export ACCESS_KEY="${ACCESS_KEY:-qEfbKkciqT2H7KLMTVt_}"
export SECRET_KEY="${SECRET_KEY:-dRX6wACNyqOJ13ZAfSX7KCj0_zj7xaI2FnKpYJQv}"

echo "Configuration:"
echo "  S3 Endpoint:      $S3_ENDPOINT"
echo "  Console Endpoint: $CONSOLE_ENDPOINT"
echo "  Test Bucket:      $TEST_BUCKET"
echo "  Access Key:       ${ACCESS_KEY:0:10}..."
echo ""

# Check if k6 is installed
if ! command -v k6 &> /dev/null; then
    echo "ERROR: k6 is not installed"
    echo "Install from: https://k6.io/docs/get-started/installation/"
    exit 1
fi

echo "✓ k6 installed: $(k6 version)"
echo ""

# Check if server is running
echo "Checking if MaxIOFS server is running..."
if ! curl -s -o /dev/null -w "%{http_code}" "$S3_ENDPOINT/health" | grep -q "200"; then
    echo "ERROR: MaxIOFS server is not responding at $S3_ENDPOINT"
    echo "Please start the server first"
    exit 1
fi
echo "✓ Server is running"
echo ""

# Clean up previous test bucket (optional)
read -p "Clean up previous test bucket? (y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    echo "Cleaning up previous test bucket..."
    aws --endpoint-url "$S3_ENDPOINT" s3 rb "s3://$TEST_BUCKET" --force 2>/dev/null || echo "  (no previous bucket found)"
fi
echo ""

# Create results directory
RESULTS_DIR="performance_results_$(date +%Y%m%d_%H%M%S)"
mkdir -p "$RESULTS_DIR"
echo "Results will be saved to: $RESULTS_DIR"
echo ""

# Function to run k6 test
run_k6_test() {
    local test_name=$1
    local test_file=$2
    local output_file="$RESULTS_DIR/${test_name}_results.json"

    echo "======================================"
    echo "Running: $test_name"
    echo "======================================"
    echo "Test file: $test_file"
    echo "Output:    $output_file"
    echo ""

    k6 run \
        --env S3_ENDPOINT="$S3_ENDPOINT" \
        --env CONSOLE_ENDPOINT="$CONSOLE_ENDPOINT" \
        --env ACCESS_KEY="$ACCESS_KEY" \
        --env SECRET_KEY="$SECRET_KEY" \
        --env TEST_BUCKET="$TEST_BUCKET" \
        --summary-export="$output_file" \
        "$test_file"

    local exit_code=$?

    if [ $exit_code -eq 0 ]; then
        echo "✓ $test_name completed successfully"
    elif [ $exit_code -eq 99 ]; then
        echo "⚠ $test_name completed with threshold warnings (exit code 99)"
    else
        echo "✗ $test_name failed with exit code $exit_code"
        return $exit_code
    fi

    echo ""
    echo "Results saved to: $output_file"
    echo ""

    return 0
}

# Test 1: Upload Performance Test
run_k6_test "upload_baseline" "tests/performance/upload_test.js"

# Test 2: Download Performance Test
run_k6_test "download_baseline" "tests/performance/download_test.js"

# Test 3: Mixed Workload Test
run_k6_test "mixed_baseline" "tests/performance/mixed_workload.js"

echo "======================================"
echo "All Performance Tests Completed!"
echo "======================================"
echo ""
echo "Results directory: $RESULTS_DIR"
echo ""
echo "Generated files:"
ls -lh "$RESULTS_DIR"/*.json
echo ""

# Create summary file
SUMMARY_FILE="$RESULTS_DIR/test_summary.txt"
cat > "$SUMMARY_FILE" << EOF
MaxIOFS Performance Test Results
=================================
Date: $(date)
Host: $(hostname)
OS: $(uname -a)
CPU: $(lscpu | grep "Model name" | cut -d':' -f2 | xargs)
Memory: $(free -h | grep Mem | awk '{print $2}')
Disk: $(df -h . | tail -1 | awk '{print $2}')

Test Configuration:
-------------------
S3 Endpoint: $S3_ENDPOINT
Console Endpoint: $CONSOLE_ENDPOINT
Test Bucket: $TEST_BUCKET

Test Files:
-----------
1. upload_baseline_results.json
2. download_baseline_results.json
3. mixed_baseline_results.json

Next Steps:
-----------
1. Copy the JSON files to your development machine
2. Share them for analysis and comparison with Windows results
3. Review the metrics and identify any differences

Command to copy results:
------------------------
scp -r $(whoami)@$(hostname):$(pwd)/$RESULTS_DIR /destination/path/

EOF

echo "Summary saved to: $SUMMARY_FILE"
cat "$SUMMARY_FILE"
echo ""
echo "======================================"
echo "Performance Testing Complete!"
echo "======================================"
echo ""
echo "To analyze results, share the files in: $RESULTS_DIR"
echo ""
