
import requests
import json

def test_follow():
    url = "http://localhost:8080/follow"
    payload = {
        "follower": "g_may29@localhost",
        "followee": "priyaa_the@localhost"
    }
    headers = {'Content-Type': 'application/json'}
    
    try:
        response = requests.post(url, json=payload, headers=headers)
        print(f"Status Code: {response.status_code}")
        print(f"Response: {response.text}")
    except Exception as e:
        print(f"Error: {e}")

if __name__ == "__main__":
    test_follow()
