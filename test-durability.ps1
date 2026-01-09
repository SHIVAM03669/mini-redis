# PowerShell script to test durability of mini-redis
# This script sets 10 keys, then verifies they can be retrieved

Write-Host "=== Mini Redis Durability Test ===" -ForegroundColor Cyan
Write-Host ""

$baseUrl = "http://localhost:8080"
$keys = @()

# Step 1: Set 10 keys
Write-Host "Step 1: Setting 10 keys..." -ForegroundColor Yellow
for ($i = 1; $i -le 10; $i++) {
    $key = "key$i"
    $value = "value$i"
    $body = @{key=$key; value=$value} | ConvertTo-Json
    
    try {
        $response = Invoke-RestMethod -Uri "$baseUrl/set" -Method Post -Body $body -ContentType "application/json"
        Write-Host "  [OK] Set $key = $value" -ForegroundColor Green
        $keys += $key
    } catch {
        Write-Host "  [FAIL] Failed to set $key : $_" -ForegroundColor Red
        exit 1
    }
}

Write-Host ""
Write-Host "Step 2: Verifying keys exist..." -ForegroundColor Yellow

# Step 2: Verify keys exist
$allFound = $true
foreach ($key in $keys) {
    try {
        $value = Invoke-RestMethod -Uri "$baseUrl/get?key=$key" -Method Get
        Write-Host "  [OK] $key = $value" -ForegroundColor Green
    } catch {
        Write-Host "  [FAIL] $key not found!" -ForegroundColor Red
        $allFound = $false
    }
}

Write-Host ""
if ($allFound) {
    Write-Host "[SUCCESS] All keys verified successfully!" -ForegroundColor Green
    Write-Host ""
    Write-Host "Next steps:" -ForegroundColor Cyan
    Write-Host "1. Kill the server (CTRL+C)" -ForegroundColor White
    Write-Host "2. Restart the server: go run ./cmd/server" -ForegroundColor White
    Write-Host "3. Run this script again to verify keys survived the restart" -ForegroundColor White
} else {
    Write-Host "[FAIL] Some keys are missing!" -ForegroundColor Red
    exit 1
}
