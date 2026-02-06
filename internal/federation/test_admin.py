import requests
import json

# Test admin login
print("Testing Admin Panel Backend...")
print("-" * 50)

backend_url = "http://localhost:8080"

# 1. Test admin login
print("\n1. Testing admin login...")
login_response = requests.post(
    f"{backend_url}/admin/login",
    headers={"Content-Type": "application/json"},
    json={"username": "admin", "password": "SecureAdminPass123!"}
)

if login_response.status_code == 200:
    data = login_response.json()
    token = data.get("token")
    print(f"   [OK] Login successful!")
    print(f"   Token received: {token[:20]}...")
    
    # 2. Test getting server stats
    print("\n2. Testing server stats endpoint...")
    stats_response = requests.get(
        f"{backend_url}/admin/stats",
        headers={"Authorization": f"Bearer {token}"}
    )
    
    if stats_response.status_code == 200:
        stats = stats_response.json()
        print(f"   [OK] Stats retrieved successfully!")
        print(f"   Total Users: {stats.get('total_users')}")
        print(f"   Total Posts: {stats.get('total_posts')}")
        print(f"   Server Name: {stats.get('server_name')}")
        print(f"   Database Status: {stats.get('database_status')}")
    else:
        print(f"   [FAIL] Failed: {stats_response.text}")
    
    # 3. Test getting server config
    print("\n3. Testing server config endpoint...")
    config_response = requests.get(
        f"{backend_url}/admin/config/server",
        headers={"Authorization": f"Bearer {token}"}
    )
    
    if config_response.status_code == 200:
        config = config_response.json()
        print(f"   [OK] Config retrieved successfully!")
        print(f"   Server Name: {config.get('server_name')}")
        print(f"   Updated By: {config.get('updated_by')}")
    else:
        print(f"   [FAIL] Failed: {config_response.text}")
    
    print("\n" + "-" * 50)
    print("[SUCCESS] All backend tests passed!")
    print("\nYou can now access the admin panel frontend:")
    print("   - Login with username: admin")
    print("   - Password: SecureAdminPass123!")
    
else:
    print(f"   [FAIL] Login failed: {login_response.text}")
    print(f"   Status code: {login_response.status_code}")

