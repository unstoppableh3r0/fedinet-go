#!/usr/bin/env python3
"""
Performance and Load Testing Script for Federation Server
Tests rate limiting, concurrent requests, and retry mechanisms
"""

import asyncio
import aiohttp
import time
import json
import sys
from collections import defaultdict
from datetime import datetime

BASE_URL = "http://localhost:8081"

class Colors:
    BLUE = '\033[94m'
    GREEN = '\033[92m'
    YELLOW = '\033[93m'
    RED = '\033[91m'
    END = '\033[0m'

class FederationLoadTester:
    def __init__(self, base_url=BASE_URL):
        self.base_url = base_url
        self.results = defaultdict(int)
        self.response_times = []
        
    async def send_request(self, session, endpoint, method="GET", data=None):
        """Send a single HTTP request"""
        url = f"{self.base_url}{endpoint}"
        start_time = time.time()
        
        try:
            if method == "GET":
                async with session.get(url) as response:
                    status = response.status
                    body = await response.text()
            else:  # POST
                headers = {"Content-Type": "application/json"}
                async with session.post(url, json=data, headers=headers) as response:
                    status = response.status
                    body = await response.text()
            
            elapsed = time.time() - start_time
            self.response_times.append(elapsed)
            return status, body, elapsed
            
        except Exception as e:
            elapsed = time.time() - start_time
            return 0, str(e), elapsed
    
    async def test_rate_limiting(self, num_requests=150):
        """Test rate limiting with concurrent requests"""
        print(f"{Colors.BLUE}╔══════════════════════════════════════╗{Colors.END}")
        print(f"{Colors.BLUE}║   Rate Limiting Load Test           ║{Colors.END}")
        print(f"{Colors.BLUE}╚══════════════════════════════════════╝{Colors.END}\n")
        
        print(f"Sending {num_requests} concurrent requests to /federation/inbox...")
        
        status_codes = defaultdict(int)
        
        async with aiohttp.ClientSession() as session:
            tasks = []
            for i in range(num_requests):
                data = {
                    "activity_type": "Test",
                    "actor": f"loadtest_{i}",
                    "actor_server": "https://loadtest.com",
                    "payload": {"test": True}
                }
                task = self.send_request(session, "/federation/inbox", "POST", data)
                tasks.append(task)
            
            results = await asyncio.gather(*tasks)
            
            for status, _, _ in results:
                status_codes[status] += 1
        
        print(f"\n{Colors.YELLOW}Results:{Colors.END}")
        for status, count in sorted(status_codes.items()):
            if status == 200:
                print(f"  {Colors.GREEN}✓ 200 OK:{Colors.END} {count} requests")
            elif status == 429:
                print(f"  {Colors.RED}✗ 429 Rate Limited:{Colors.END} {count} requests")
            else:
                print(f"  {Colors.YELLOW}⚠ {status}:{Colors.END} {count} requests")
        
        if status_codes[429] > 0:
            print(f"\n{Colors.GREEN}✓ Rate limiting is working correctly!{Colors.END}")
        else:
            print(f"\n{Colors.YELLOW}⚠ Warning: Rate limiting may not be enforced{Colors.END}")
    
    async def test_concurrent_inbox(self, num_concurrent=50):
        """Test concurrent inbox requests"""
        print(f"\n{Colors.BLUE}╔══════════════════════════════════════╗{Colors.END}")
        print(f"{Colors.BLUE}║   Concurrent Inbox Test              ║{Colors.END}")
        print(f"{Colors.BLUE}╚══════════════════════════════════════╝{Colors.END}\n")
        
        print(f"Sending {num_concurrent} concurrent valid requests...")
        
        start = time.time()
        
        async with aiohttp.ClientSession() as session:
            tasks = []
            for i in range(num_concurrent):
                data = {
                    "activity_type": "Post",
                    "actor": f"user_{i}",
                    "actor_server": f"https://server{i % 10}.com",
                    "payload": {
                        "content": f"Test post {i}",
                        "timestamp": datetime.now().isoformat()
                    }
                }
                task = self.send_request(session, "/federation/inbox", "POST", data)
                tasks.append(task)
            
            results = await asyncio.gather(*tasks)
        
        elapsed = time.time() - start
        
        success_count = sum(1 for status, _, _ in results if status == 200)
        avg_response_time = sum(rt for _, _, rt in results) / len(results)
        
        print(f"\n{Colors.YELLOW}Results:{Colors.END}")
        print(f"  Total requests: {num_concurrent}")
        print(f"  Successful: {Colors.GREEN}{success_count}{Colors.END}")
        print(f"  Failed: {Colors.RED}{num_concurrent - success_count}{Colors.END}")
        print(f"  Total time: {elapsed:.2f}s")
        print(f"  Avg response time: {avg_response_time*1000:.2f}ms")
        print(f"  Throughput: {num_concurrent/elapsed:.2f} req/s")
    
    async def test_retry_queue_simulation(self):
        """Simulate failed deliveries to test retry queue"""
        print(f"\n{Colors.BLUE}╔══════════════════════════════════════╗{Colors.END}")
        print(f"{Colors.BLUE}║   Retry Queue Simulation             ║{Colors.END}")
        print(f"{Colors.BLUE}╚══════════════════════════════════════╝{Colors.END}\n")
        
        print("Sending activities to non-existent servers...")
        
        async with aiohttp.ClientSession() as session:
            tasks = []
            for i in range(10):
                data = {
                    "activity_type": "Follow",
                    "actor_id": f"user{i}@localhost",
                    "target_server": f"https://fake-server-{i}.invalid",
                    "payload": {"test": True}
                }
                task = self.send_request(session, "/federation/send", "POST", data)
                tasks.append(task)
            
            results = await asyncio.gather(*tasks)
        
        success_count = sum(1 for status, _, _ in results if status in [200, 201])
        
        print(f"\n{Colors.YELLOW}Results:{Colors.END}")
        print(f"  Activities queued: {Colors.GREEN}{success_count}{Colors.END}/10")
        print(f"  {Colors.YELLOW}Note:{Colors.END} Check server logs for retry attempts")
        print(f"  {Colors.YELLOW}Note:{Colors.END} Retry worker runs every 30 seconds")
    
    async def test_health_monitoring(self):
        """Monitor health endpoint during load"""
        print(f"\n{Colors.BLUE}╔══════════════════════════════════════╗{Colors.END}")
        print(f"{Colors.BLUE}║   Health Monitoring Test             ║{Colors.END}")
        print(f"{Colors.BLUE}╚══════════════════════════════════════╝{Colors.END}\n")
        
        async with aiohttp.ClientSession() as session:
            # Take initial health snapshot
            status1, body1, _ = await self.send_request(session, "/federation/health")
            health1 = json.loads(body1) if status1 == 200 else {}
            
            # Generate some load
            print("Generating load...")
            tasks = []
            for i in range(100):
                data = {
                    "activity_type": "Like",
                    "actor": f"loadtest_{i}",
                    "actor_server": "https://loadtest.com",
                    "payload": {}
                }
                task = self.send_request(session, "/federation/inbox", "POST", data)
                tasks.append(task)
            
            await asyncio.gather(*tasks)
            
            # Take final health snapshot
            await asyncio.sleep(1)
            status2, body2, _ = await self.send_request(session, "/federation/health")
            health2 = json.loads(body2) if status2 == 200 else {}
        
        print(f"\n{Colors.YELLOW}Health Metrics:{Colors.END}")
        if health1 and health2:
            print(f"  Status: {health2.get('status', 'unknown')}")
            
            total_increase = health2.get('total_messages', 0) - health1.get('total_messages', 0)
            print(f"  Messages processed: {Colors.GREEN}+{total_increase}{Colors.END}")
            
            latency = health2.get('average_latency_ms', 0)
            print(f"  Avg latency: {latency}ms")
        else:
            print(f"  {Colors.RED}Failed to retrieve health metrics{Colors.END}")
    
    async def run_all_tests(self):
        """Run complete test suite"""
        print(f"\n{Colors.BLUE}╔════════════════════════════════════════════╗{Colors.END}")
        print(f"{Colors.BLUE}║   Federation Load Testing Suite            ║{Colors.END}")
        print(f"{Colors.BLUE}╚════════════════════════════════════════════╝{Colors.END}\n")
        
        # Check server availability
        async with aiohttp.ClientSession() as session:
            try:
                status, _, _ = await self.send_request(session, "/federation/health")
                if status != 200:
                    print(f"{Colors.RED}✗ Federation server is not responding{Colors.END}")
                    print(f"  Please start the server: cd internal/federation && go run .")
                    return
            except Exception as e:
                print(f"{Colors.RED}✗ Cannot connect to federation server{Colors.END}")
                print(f"  Error: {e}")
                return
        
        print(f"{Colors.GREEN}✓ Federation server is running{Colors.END}\n")
        
        # Run tests
        await self.test_health_monitoring()
        await self.test_concurrent_inbox(50)
        await self.test_rate_limiting(150)
        await self.test_retry_queue_simulation()
        
        # Summary
        print(f"\n{Colors.BLUE}╔═══════════════════════════════════════════╗{Colors.END}")
        print(f"{Colors.BLUE}║   Test Summary                             ║{Colors.END}")
        print(f"{Colors.BLUE}╚═══════════════════════════════════════════╝{Colors.END}\n")
        
        if self.response_times:
            avg_rt = sum(self.response_times) / len(self.response_times)
            min_rt = min(self.response_times)
            max_rt = max(self.response_times)
            
            print(f"  Total requests: {len(self.response_times)}")
            print(f"  Avg response time: {avg_rt*1000:.2f}ms")
            print(f"  Min response time: {min_rt*1000:.2f}ms")
            print(f"  Max response time: {max_rt*1000:.2f}ms")
        
        print(f"\n{Colors.GREEN}✓ All load tests completed{Colors.END}\n")

async def main():
    tester = FederationLoadTester()
    await tester.run_all_tests()

if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        print(f"\n{Colors.YELLOW}⚠ Tests interrupted by user{Colors.END}")
        sys.exit(1)
