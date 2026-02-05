# Timeline Tests

Comprehensive test documentation and templates for the FediNet Timeline Service.

## 📁 Test Organization

### Runnable Tests (in parent directory)

The actual executable unit tests are in:

- **`../examples_test.go`** - Complete test suite with passing tests

### Test Documentation (this directory)

This directory contains:

- **Test scenario documentation** - Detailed test case descriptions
- **API usage templates** - Examples of how to test each endpoint
- **Integration test patterns** - Templates for future E2E tests

## 🧪 Running Tests

### Run the Actual Test Suite

```bash
cd /Users/mithresh/Desktop/Sparkle/VI_SEM/SoftEng/fedinet-go/internal/timeline
go test -v
```

### Test This Directory

```bash
cd /Users/mithresh/Desktop/Sparkle/VI_SEM/SoftEng/fedinet-go/internal/timeline/test
go test -v
```

## 📚 Test Documentation Files

### 1. Test Scenario Documentation

The following files document comprehensive test scenarios for each user story:

#### `ranking_test.go` Documentation

Tests for **User Story 4.9: Customizable Ranking**

**Features Tested:**

- Setting ranking preferences (chronological, popular, relevance, trending)
- Getting user ranking preferences
- Timeline retrieval with different ranking modes
- Invalid ranking mode handling
- Response format validation
- Benchmarks for ranking operations

**Test Coverage:**

- ✓ Valid ranking modes
- ✓ Invalid ranking modes
- ✓ Missing parameters
- ✓ JSON validation
- ✓ Pagination
- ✓ Default preferences

---

### 2. `versioning_test.go`

Tests for **User Story 4.13: Post Versioning**

**Features Tested:**

- Post editing with version creation
- Version history retrieval
- Specific version access
- Multiple sequential edits
- Change notes and timestamps

**Test Coverage:**

- ✓ Valid post edits
- ✓ Missing required fields
- ✓ Version numbering
- ✓ Version ordering (descending)
- ✓ Non-existent posts/versions
- ✓ Response structure validation

---

### 3. `offline_test.go`

Tests for **User Story 4.14: Offline Mode**

**Features Tested:**

- Timeline caching
- Cache retrieval
- Cache refresh
- Size limit enforcement
- Cache expiration
- Complete offline workflow

**Test Coverage:**

- ✓ Cache creation
- ✓ Cache retrieval (fresh and expired)
- ✓ Cache refresh
- ✓ Size limits (50MB, 500 posts)
- ✓ Missing cache handling
- ✓ Response format validation

---

### 4. `adaptive_refresh_test.go`

Tests for **User Story 4.15: Adaptive Feed Refresh**

**Features Tested:**

- Refresh interval calculation
- User activity tracking
- Server load recording
- Activity level detection
- Load level detection
- Adaptive workflow

**Test Coverage:**

- ✓ Activity levels (high, medium, low, idle)
- ✓ Load levels (normal, high, critical)
- ✓ Interval adaptation
- ✓ Throttling mechanisms
- ✓ Activity timestamp updates
- ✓ Complete adaptive workflow

---

### 5. `integration_test.go`

End-to-end integration tests

**Scenarios Tested:**

- Complete user journey (8 steps)
- Ranking mode comparisons
- Multiple sequential edits
- Concurrent requests
- Stress testing under load

**Test Coverage:**

- ✓ Full user workflows
- ✓ Cross-feature integration
- ✓ Concurrency handling
- ✓ Performance under load
- ✓ System adaptation

---

### 6. `test_helpers.go`

Testing utilities and fixtures

**Utilities Provided:**

- `TestHelper` - Assertion functions
- `MockTimeProvider` - Time mocking
- `MockPost` - Test data generation
- `TestDataFixture` - Common test data
- `ScenarioBuilder` - Scenario construction
- `PerformanceTracker` - Performance metrics

---

## Running Tests

### Run All Tests

```bash
cd /Users/mithresh/Desktop/Sparkle/VI_SEM/SoftEng/fedinet-go/internal/timeline/test
go test -v
```

### Run Specific Test File

```bash
go test -v -run TestRanking ranking_test.go
go test -v -run TestVersioning versioning_test.go
go test -v -run TestOffline offline_test.go
go test -v -run TestAdaptive adaptive_refresh_test.go
go test -v -run TestIntegration integration_test.go
```

### Run Specific Test

```bash
go test -v -run TestSetRankingPreference
go test -v -run TestEditPost
go test -v -run TestCacheTimeline
go test -v -run TestGetRefreshInterval
```

### Run Benchmarks

```bash
go test -bench=. -benchmem
```

### Run with Coverage

```bash
go test -v -cover -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### Run Integration Tests Only

```bash
go test -v -run Integration
```

### Run Quick Tests (Skip Long Tests)

```bash
go test -v -short
```

---

## Test Structure

Each test file follows this pattern:

```go
### Basic Test
func TestFeatureName(t *testing.T) {
    tests := []struct {
        name           string
        input          interface{}
        expectedStatus int
    }{
        // Test cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test logic
        })
    }
}

// Benchmark
func BenchmarkFeatureName(b *testing.B) {
    for i := 0; i < b.N; i++ {
        // Operation to benchmark
    }
}
```

---

## Test Data

### Mock Users

- `alice@server1.com`
- `bob@server2.com`
- `charlie@server3.com`
- `david@server1.com`
- `eve@server2.com`

### Ranking Modes

- `chronological`
- `popular`
- `relevance`
- `trending`

### Activity Levels

- `high` (< 5 min)
- `medium` (< 15 min)
- `low` (< 1 hour)
- `idle` (> 1 hour)

### Load Levels

- `normal` (CPU < 60%, Mem < 60%, RPS < 500)
- `high` (CPU > 60%, Mem > 60%, RPS > 500)
- `critical` (CPU > 80%, Mem > 80%, RPS > 1000)

---

## Example Test Output

```
=== RUN   TestSetRankingPreference
=== RUN   TestSetRankingPreference/Valid_chronological_preference
    ranking_test.go:20: Test case: Valid chronological preference
    ranking_test.go:21: Request body: {"user_id":"alice","preference":"chronological"}
    ranking_test.go:22: Expected status: 200
=== RUN   TestSetRankingPreference/Valid_trending_preference
    ranking_test.go:20: Test case: Valid trending preference
    ranking_test.go:21: Request body: {"user_id":"bob","preference":"trending"}
    ranking_test.go:22: Expected status: 200
--- PASS: TestSetRankingPreference (0.01s)
    --- PASS: TestSetRankingPreference/Valid_chronological_preference (0.00s)
    --- PASS: TestSetRankingPreference/Valid_trending_preference (0.00s)
PASS
```

---

## Integration with CI/CD

### GitHub Actions Example

```yaml
name: Timeline Tests
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v2
      - uses: actions/setup-go@v2
        with:
          go-version: "1.21"
      - name: Run tests
        run: |
          cd internal/timeline/test
          go test -v -cover
```

---

## Test Coverage Goals

- **Unit Tests:** 80%+ coverage
- **Integration Tests:** Key workflows covered
- **Edge Cases:** All error paths tested
- **Performance:** Benchmarks for critical operations

---

## Best Practices

1. **Use Table-Driven Tests**

   - Easier to add new test cases
   - Similar to read and maintain

2. **Test Error Paths**

   - Invalid inputs
   - Missing parameters
   - Edge cases

3. **Mock External Dependencies**

   - Use test helpers
   - Avoid actual database calls in unit tests

4. **Clear Test Names**

   - Descriptive test case names
   - Use subtests for organization

5. **Performance Testing**
   - Benchmark critical paths
   - Track performance over time

---

## Troubleshooting

### Tests Failing?

1. **Check Database Connection**

   ```bash
   export DATABASE_URL="postgresql://localhost:5432/fedinet_timeline_test?sslmode=disable"
   ```

2. **Ensure Service is Not Running**

   - Tests may conflict with running service on port 8081

3. **Clean Test Data**
   ```sql
   TRUNCATE TABLE user_ranking_preferences CASCADE;
   TRUNCATE TABLE post_versions CASCADE;
   TRUNCATE TABLE cached_timelines CASCADE;
   ```

### Integration Tests Timeout?

- Increase timeout in `makeRequest` helper
- Check network connectivity
- Verify test server initialization

---

## Contributing

When adding new tests:

1. Follow existing test structure
2. Add test documentation
3. Include both success and failure cases
4. Add benchmarks for performance-critical code
5. Update this README

---

## Test Metrics

Run this to see test statistics:

```bash
go test -v 2>&1 | grep -E "PASS|FAIL|RUN" | wc -l
```

Generate coverage report:

```bash
go test -coverprofile=coverage.out
go tool cover -func=coverage.out
```

---

## Summary

**Total Test Files:** 6  
**Test Categories:** 4 (Ranking, Versioning, Offline, Adaptive)  
**Integration Scenarios:** 5+  
**Test Helpers:** 6 utilities  
**Coverage:** Unit + Integration + E2E

All tests are designed to be:

- ✓ Fast (< 1s per test)
- ✓ Isolated (no dependencies)
- ✓ Repeatable (consistent results)
- ✓ Clear (easy to understand)
- ✓ Maintainable (easy to update)

---

**Status:** Ready for Testing ✅
