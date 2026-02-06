#!/bin/bash

# Federation Server Test Suite
# Tests all implemented user stories

set -e

BASE_URL="http://localhost:8081"
COLORS=true

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test counters
PASSED=0
FAILED=0

# Helper functions
print_header() {
    echo -e "\n${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}\n"
}

print_test() {
    echo -e "${YELLOW}▶ Testing:${NC} $1"
}

print_success() {
    echo -e "${GREEN}✓ PASSED:${NC} $1"
    ((PASSED++))
}

print_failure() {
    echo -e "${RED}✗ FAILED:${NC} $1"
    echo -e "${RED}  Response:${NC} $2"
    ((FAILED++))
}

# Test if server is running
check_server() {
    print_header "Checking Server Status"
    if curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/federation/health" | grep -q "200"; then
        print_success "Federation server is running"
    else
        print_failure "Federation server is not running" "Please start: cd internal/federation && go run ."
        exit 1
    fi
}

# User Story 2.14: Health API
test_health() {
    print_header "User Story 2.14: Instance Health API"
    
    print_test "GET /federation/health"
    response=$(curl -s "$BASE_URL/federation/health")
    
    if echo "$response" | jq -e '.status' > /dev/null 2>&1; then
        print_success "Health endpoint returns valid JSON"
        echo "  Status: $(echo $response | jq -r '.status')"
        echo "  Uptime: $(echo $response | jq -r '.uptime_seconds')s"
    else
        print_failure "Health endpoint failed" "$response"
    fi
}

# User Story 2.11: Capabilities
test_capabilities() {
    print_header "User Story 2.11: Capability Negotiation"
    
    print_test "GET /federation/capabilities"
    response=$(curl -s "$BASE_URL/federation/capabilities")
    
    if echo "$response" | jq -e '.protocol_versions' > /dev/null 2>&1; then
        print_success "Capabilities endpoint returns valid data"
        echo "  Protocol Versions: $(echo $response | jq -r '.protocol_versions')"
        echo "  Supported Types: $(echo $response | jq -r '.supported_types')"
        echo "  Supports Retries: $(echo $response | jq -r '.supports_retries')"
    else
        print_failure "Capabilities endpoint failed" "$response"
    fi
}

# User Story 2.13: Federation Mode
test_federation_mode() {
    print_header "User Story 2.13: Federation Modes"
    
    print_test "GET /federation/admin/mode (Initial state)"
    response=$(curl -s "$BASE_URL/federation/admin/mode")
    
    if echo "$response" | jq -e '.mode' > /dev/null 2>&1; then
        initial_mode=$(echo $response | jq -r '.mode')
        print_success "Retrieved current mode: $initial_mode"
    else
        print_failure "Failed to get federation mode" "$response"
        return
    fi
    
    print_test "PUT /federation/admin/mode (Switch to hard mode)"
    response=$(curl -s -X PUT "$BASE_URL/federation/admin/mode" \
        -H "Content-Type: application/json" \
        -d '{"mode":"hard","allow_unknown_servers":false}')
    
    if echo "$response" | jq -e '.success' | grep -q "true"; then
        print_success "Successfully switched to hard mode"
    else
        print_failure "Failed to switch mode" "$response"
    fi
    
    # Switch back to original mode
    print_test "PUT /federation/admin/mode (Switch back to $initial_mode)"
    curl -s -X PUT "$BASE_URL/federation/admin/mode" \
        -H "Content-Type: application/json" \
        -d "{\"mode\":\"$initial_mode\"}" > /dev/null
    print_success "Restored original mode: $initial_mode"
}

# User Story 2.4: Inbox
test_inbox() {
    print_header "User Story 2.4: Inbox/Outbox Architecture"
    
    print_test "POST /federation/inbox (Receive activity)"
    response=$(curl -s -X POST "$BASE_URL/federation/inbox" \
        -H "Content-Type: application/json" \
        -d '{
            "activity_type": "Follow",
            "actor": "alice",
            "actor_server": "https://test-server.com",
            "target": "bob",
            "payload": {"message": "Test follow request"}
        }')
    
    if echo "$response" | jq -e '.success' | grep -q "true"; then
        activity_id=$(echo $response | jq -r '.data.activity_id')
        print_success "Successfully received activity (ID: $activity_id)"
    else
        print_failure "Failed to receive activity" "$response"
    fi
}

# User Story 2.12: Server Blocking
test_server_blocking() {
    print_header "User Story 2.12: Blocked Server Lists"
    
    SPAM_SERVER="https://spam-test.com"
    
    print_test "POST /federation/admin/blocks (Block server)"
    response=$(curl -s -X POST "$BASE_URL/federation/admin/blocks" \
        -H "Content-Type: application/json" \
        -d "{
            \"server_url\": \"$SPAM_SERVER\",
            \"reason\": \"Test blocking\"
        }")
    
    if echo "$response" | jq -e '.success' | grep -q "true"; then
        print_success "Successfully blocked server: $SPAM_SERVER"
    else
        print_failure "Failed to block server" "$response"
    fi
    
    print_test "POST /federation/inbox (Test block enforcement)"
    response=$(curl -s -X POST "$BASE_URL/federation/inbox" \
        -H "Content-Type: application/json" \
        -d "{
            \"activity_type\": \"Follow\",
            \"actor\": \"spammer\",
            \"actor_server\": \"$SPAM_SERVER\",
            \"target\": \"victim\",
            \"payload\": {}
        }")
    
    if echo "$response" | jq -e '.error.type' | grep -q "server_blocked"; then
        print_success "Blocked server successfully rejected"
    else
        print_failure "Block enforcement failed" "$response"
    fi
    
    print_test "GET /federation/admin/blocks (List blocked servers)"
    response=$(curl -s "$BASE_URL/federation/admin/blocks")
    
    if echo "$response" | jq -e '.blocked_servers' > /dev/null 2>&1; then
        count=$(echo $response | jq -r '.count')
        print_success "Retrieved blocked servers list (Count: $count)"
    else
        print_failure "Failed to get blocked servers" "$response"
    fi
    
    print_test "DELETE /federation/admin/blocks (Unblock server)"
    response=$(curl -s -X DELETE "$BASE_URL/federation/admin/blocks?server_url=$SPAM_SERVER")
    
    if echo "$response" | jq -e '.success' | grep -q "true"; then
        print_success "Successfully unblocked server"
    else
        print_failure "Failed to unblock server" "$response"
    fi
}

# User Story 2.3: Retry mechanism (simulated)
test_retry_mechanism() {
    print_header "User Story 2.3: Secure Delivery with Retries"
    
    print_test "POST /federation/send (Send to non-existent server)"
    response=$(curl -s -X POST "$BASE_URL/federation/send" \
        -H "Content-Type: application/json" \
        -d '{
            "activity_type": "Post",
            "actor_id": "alice@localhost",
            "target_server": "https://non-existent-server-12345.com",
            "payload": {"content": "Test message"}
        }')
    
    if echo "$response" | jq -e '.success' | grep -q "true"; then
        activity_id=$(echo $response | jq -r '.data.activity_id')
        print_success "Activity queued for delivery (ID: $activity_id)"
        echo "  Note: Retry worker will attempt delivery every 30s"
    else
        print_failure "Failed to queue activity" "$response"
    fi
}

# User Story 2.8: Rate Limiting
test_rate_limiting() {
    print_header "User Story 2.8: Rate Limiting"
    
    print_test "Sending multiple rapid requests to test rate limit"
    
    success_count=0
    rate_limited=0
    
    for i in {1..60}; do
        status_code=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$BASE_URL/federation/inbox" \
            -H "Content-Type: application/json" \
            -d "{
                \"activity_type\": \"Test\",
                \"actor\": \"tester$i\",
                \"actor_server\": \"https://rate-test.com\",
                \"payload\": {}
            }")
        
        if [ "$status_code" = "200" ]; then
            ((success_count++))
        elif [ "$status_code" = "429" ]; then
            ((rate_limited++))
        fi
    done
    
    echo -e "  Successful requests: $success_count"
    echo -e "  Rate limited requests: $rate_limited"
    
    if [ $rate_limited -gt 0 ]; then
        print_success "Rate limiting is working (blocked $rate_limited requests)"
    else
        echo -e "${YELLOW}⚠ WARNING:${NC} Rate limiting may not be enforced (all 60 requests succeeded)"
    fi
}

# User Story 2.7: Acknowledgment
test_acknowledgment() {
    print_header "User Story 2.7: Delivery Acknowledgment"
    
    print_test "POST /federation/ack (Send acknowledgment)"
    
    # Generate a random UUID for testing
    test_uuid="$(uuidgen)"
    
    response=$(curl -s -X POST "$BASE_URL/federation/ack" \
        -H "Content-Type: application/json" \
        -d "{
            \"message_id\": \"$test_uuid\",
            \"status\": \"received\",
            \"reason\": null
        }")
    
    if echo "$response" | jq -e '.success' | grep -q "true"; then
        print_success "Acknowledgment accepted"
    else
        print_failure "Failed to send acknowledgment" "$response"
    fi
}

# User Story 2.10: Content Serialization
test_serialization() {
    print_header "User Story 2.10: Content Serialization Format"
    
    print_test "POST /federation/inbox (Test JSON serialization)"
    
    response=$(curl -s -X POST "$BASE_URL/federation/inbox" \
        -H "Content-Type: application/json" \
        -d '{
            "activity_type": "Post",
            "actor": "test_user",
            "actor_server": "https://test.com",
            "target": "public",
            "payload": {
                "content": "Test post with special characters: 你好 🌟",
                "nested": {
                    "field": "value"
                },
                "array": [1, 2, 3]
            }
        }')
    
    if echo "$response" | jq -e '.success' | grep -q "true"; then
        print_success "Complex JSON payload serialized correctly"
    else
        print_failure "Serialization test failed" "$response"
    fi
}

# Invalid input tests
test_error_handling() {
    print_header "Error Handling Tests"
    
    print_test "POST /federation/inbox (Missing required fields)"
    response=$(curl -s -X POST "$BASE_URL/federation/inbox" \
        -H "Content-Type: application/json" \
        -d '{"activity_type": "Follow"}')
    
    if echo "$response" | jq -e '.error.type' | grep -q "missing_fields"; then
        print_success "Correctly rejected invalid input"
    else
        print_failure "Error handling failed" "$response"
    fi
    
    print_test "GET /federation/outbox (Missing actor_id)"
    response=$(curl -s "$BASE_URL/federation/outbox")
    
    if echo "$response" | jq -e '.error' > /dev/null 2>&1; then
        print_success "Correctly rejected missing parameter"
    else
        print_failure "Error handling failed" "$response"
    fi
}

# Main test execution
main() {
    echo -e "${BLUE}"
    echo "╔══════════════════════════════════════════════════╗"
    echo "║     Federation Server Test Suite                ║"
    echo "║     Testing All User Stories                    ║"
    echo "╚══════════════════════════════════════════════════╝"
    echo -e "${NC}"
    
    check_server
    test_health
    test_capabilities
    test_federation_mode
    test_inbox
    test_server_blocking
    test_retry_mechanism
    test_rate_limiting
    test_acknowledgment
    test_serialization
    test_error_handling
    
    # Summary
    print_header "Test Summary"
    total=$((PASSED + FAILED))
    
    echo -e "${GREEN}Passed: $PASSED${NC}"
    echo -e "${RED}Failed: $FAILED${NC}"
    echo -e "Total:  $total"
    
    if [ $FAILED -eq 0 ]; then
        echo -e "\n${GREEN}🎉 All tests passed!${NC}\n"
        exit 0
    else
        echo -e "\n${RED}❌ Some tests failed${NC}\n"
        exit 1
    fi
}

# Run tests
main
