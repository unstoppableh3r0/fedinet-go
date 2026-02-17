$ErrorActionPreference = "Stop"

function Assert-Success($response, $context) {
    if ($response.status -eq "ok" -or $response.success -eq $true -or $response.id) {
        Write-Host "✅ $context - Success" -ForegroundColor Green
    } else {
        Write-Host "❌ $context - Failed" -ForegroundColor Red
        Write-Host ($response | ConvertTo-Json -Depth 5)
        exit 1
    }
}

Write-Host "`n=== 1. Checking Server Health ===" -ForegroundColor Cyan
try {
    $healthA = Invoke-RestMethod -Uri "http://localhost:8080/health"
    Assert-Success $healthA "Server A Health"
    
    $healthB = Invoke-RestMethod -Uri "http://localhost:9080/health"
    Assert-Success $healthB "Server B Health"
} catch {
    Write-Host "❌ Health check failed: $_" -ForegroundColor Red
    exit 1
}

Write-Host "`n=== 2. Ensuring Users Exist ===" -ForegroundColor Cyan
# Ensure Alice exists on Server A
$aliceParams = @{
    username = "alice"
    password = "password123"
    email = "alice@example.com"
}
try {
    Invoke-RestMethod -Uri "http://localhost:8080/register" -Method Post -Body ($aliceParams | ConvertTo-Json) -ContentType "application/json" | Out-Null
    Write-Host "✅ Alice registered on Server A" -ForegroundColor Green
} catch {
    Write-Host "ℹ️ Alice likely already exists on Server A" -ForegroundColor Yellow
}

# Ensure Bob exists on Server B
$bobParams = @{
    username = "bob"
    password = "password123"
    email = "bob@example.com"
}
try {
    Invoke-RestMethod -Uri "http://localhost:9080/register" -Method Post -Body ($bobParams | ConvertTo-Json) -ContentType "application/json" | Out-Null
    Write-Host "✅ Bob registered on Server B" -ForegroundColor Green
} catch {
    Write-Host "ℹ️ Bob likely already exists on Server B" -ForegroundColor Yellow
}

Write-Host "`n=== 3. Sending Message: Alice (A) -> Bob (B) ===" -ForegroundColor Cyan
$messagePayload = @{
    activity_type = "Message"
    actor_id = "alice@localhost"
    target_server = "http://server_b_federation:8081"
    target_id = "bob@localhost"
    payload = @{
        content = "Hello Bob from Server A!"
        timestamp = (Get-Date).ToString("yyyy-MM-ddTHH:mm:ssZ")
    }
}

$sendResponse = Invoke-RestMethod -Uri "http://localhost:8081/federation/send" -Method Post -Body ($messagePayload | ConvertTo-Json) -ContentType "application/json"
Assert-Success $sendResponse "Send Message"
$activityID = $sendResponse.data.activity_id
Write-Host "   Activity ID: $activityID" -ForegroundColor Gray

Start-Sleep -Seconds 2

Write-Host "`n=== 4. Checking Alice's Outbox (Server A) ===" -ForegroundColor Cyan
$outbox = Invoke-RestMethod -Uri "http://localhost:8081/federation/outbox?actor_id=alice@localhost"
$foundOutbox = $false
foreach ($msg in $outbox.activities) {
    if ($msg.id -eq $activityID) {
        Write-Host "✅ Message found in Outbox" -ForegroundColor Green
        Write-Host "   Status: $($msg.delivery_status)" -ForegroundColor Gray
        $foundOutbox = $true
        break
    }
}
if (-not $foundOutbox) {
    Write-Host "❌ Message NOT found in Outbox" -ForegroundColor Red
}

Write-Host "`n=== 5. Checking Bob's Inbox (Server B) ===" -ForegroundColor Cyan
# Note: In a real scenario we'd query by target_id, but the endpoint might just dump all for the demo or require auth
# Checking Server B's federation inbox directly (simulated as internal inspection)
# The current InboxHandler doesn't seem to expose a "Get Inbox" API generally, but we can check the database via the federation outbox endpoint? 
# Wait, Server B has its OWN database.
# Let's inspect Server B's database directly via docker exec to prove receipt
$checkCmd = "docker exec -i fedinet_postgres psql -U postgres -d fedinet_server_b -c ""SELECT id, activity_type, actor_id, status FROM inbox_activities WHERE payload->>'content' = 'Hello Bob from Server A!'"""
$inboxCheck = Invoke-Expression $checkCmd

if ($inboxCheck -match "Message") {
    Write-Host "✅ Message received in Server B Inbox!" -ForegroundColor Green
    $inboxCheck | Write-Host
} else {
    Write-Host "❌ Message NOT found in Server B Inbox" -ForegroundColor Red
    Write-Host "Debug output:"
    $inboxCheck | Write-Host
}

Write-Host "`n=== TEST COMPLETE ===" -ForegroundColor Green
