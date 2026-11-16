# Demo data seed script (idempotent): walks the real business flow.
# Usage: powershell -File docs/seed-demo.ps1
$ErrorActionPreference = 'Stop'
$base = 'http://localhost:8000'

function Invoke-Api($method, $path, $headers, $body) {
    $opt = @{ Method = $method; Uri = $base + $path; Headers = $headers; ContentType = 'application/json; charset=utf-8'; UseBasicParsing = $true }
    if ($body) { $opt.Body = [System.Text.Encoding]::UTF8.GetBytes(($body | ConvertTo-Json -Compress)) }
    $res = Invoke-WebRequest @opt
    return ($res.Content | ConvertFrom-Json)
}

$user = @{ 'X-User-Id' = 'user_1001' }
$user2 = @{ 'X-User-Id' = 'user_1002' }
$mgr = @{ 'X-Staff-Id' = 'manager_01' }
$front = @{ 'X-Staff-Id' = 'front_01' }
$svc = @{ 'X-Staff-Id' = 'svc_01' }

function Find-Unpaid($headers, $slotStart) {
    $r = Invoke-Api 'GET' '/api/user/app/list?status=1&page=1&size=50' $headers $null
    if ($r.code -ne 0) { return $null }
    $iso = $slotStart -replace ' ', 'T'
    $hit = $r.data.list | Where-Object { $_.startTime -like "$iso*" -or $_.startTime -like "$slotStart*" } | Select-Object -First 1
    if ($hit) { return $hit.appId }
    return $null
}

function Pay-Existing($headers, $appId, $key) {
    $p = Invoke-Api 'POST' '/api/pay/create' $headers @{ appId = $appId }
    if ($p.code -ne 0) { return $false }
    $paymentId = $p.data.paymentId
    if (-not $paymentId) { $paymentId = $p.data }
    $n = Invoke-Api 'POST' '/api/pay/mock/notify' $null @{ paymentId = $paymentId; transactionId = "TXN_$key"; success = $true; amount = 9900 }
    return ($n.code -eq 0)
}

function Find-By-Status($headers, $slotStart, $status) {
    $r = Invoke-Api 'GET' "/api/user/app/list?status=$status&page=1&size=50" $headers $null
    if ($r.code -ne 0) { return $null }
    $iso = $slotStart -replace ' ', 'T'
    $hit = $r.data.list | Where-Object { $_.startTime -like "$iso*" -or $_.startTime -like "$slotStart*" } | Select-Object -First 1
    if ($hit) { return $hit.appId }
    return $null
}

function Book-Or-Reuse($headers, $petId, $slotId, $slotStart, $key) {
    $r = Invoke-Api 'POST' '/api/user/app/book' $headers @{ petId = $petId; slotId = $slotId; idemKey = $key }
    if ($r.code -eq 0) {
        $appId = $r.data.appId
    } else {
        $done = Find-By-Status $headers $slotStart 2
        if ($done) { Write-Host "[SKIP] already confirmed $done"; return $done }
        Write-Host "[INFO] book returned: $($r.message) - reusing existing unpaid order"
        $appId = Find-Unpaid $headers $slotStart
        if (-not $appId) { throw "cannot find reusable order for slot $slotStart" }
    }
    if (-not (Pay-Existing $headers $appId "$key")) {
        # maybe previous unpaid order expired and got closed; retry a fresh booking
        $r2 = Invoke-Api 'POST' '/api/user/app/book' $headers @{ petId = $petId; slotId = $slotId; idemKey = $key + '_retry' }
        if ($r2.code -ne 0) { throw "book retry failed: $($r2.message)" }
        $appId = $r2.data.appId
        if (-not (Pay-Existing $headers $appId "$key")) { throw "pay retry failed for $appId" }
    }
    Write-Host "[OK] booked+paid $appId (pet=$petId slot=$slotId)"
    return $appId
}

# ---------- 1. two orders waiting verify (slot_02, today 10:00) ----------
Book-Or-Reuse $user 'pet_2001' 'slot_02' '2026-11-16 10:00' "seed_verify_1_$(Get-Random)" | Out-Null
Book-Or-Reuse $user 'pet_2002' 'slot_02' '2026-11-16 10:00' "seed_verify_2_$(Get-Random)" | Out-Null

# ---------- 2. two finished orders (today 14:00): verify -> start -> finish ----------
$a1 = Book-Or-Reuse $user 'pet_2001' 'slot_oyv0gg01hscdmvxd8w9mg3s200nc3xcn' '2026-11-16 14:00' "seed_fin_1_$(Get-Random)"
$a2 = Book-Or-Reuse $user2 'pet_2003' 'slot_oyv0gg01hscdmvxd8w9mg3s200nc3xcn' '2026-11-16 14:00' "seed_fin_2_$(Get-Random)"
foreach ($a in @($a1, $a2)) {
    foreach ($step in @(@('POST', '/api/shop/app/verify', $front), @('POST', '/api/shop/app/service/start', $svc), @('POST', '/api/shop/app/service/finish', $svc))) {
        $r = Invoke-Api $step[0] $step[1] $step[2] @{ appId = $a }
        if ($r.code -ne 0) { Write-Host "[SKIP] $a step $($step[1]): $($r.message)" }
    }
    Write-Host "[OK] processed $a"
}

# ---------- 3. one cancel-audit order (fresh slot soon, <2h cancel -> audit) ----------
$st = (Get-Date).AddMinutes(10).ToString('yyyy-MM-dd HH:mm:ss')
$et = (Get-Date).AddMinutes(69).ToString('yyyy-MM-dd HH:mm:ss')
$s = Invoke-Api 'POST' '/api/shop/slot/create' $mgr @{ startTime = $st; endTime = $et }
if ($s.code -ne 0) { throw "slot create failed: $($s.message)" }
$auditSlot = $s.data.slotId
if (-not $auditSlot) { $auditSlot = $s.data }
$a3 = Book-Or-Reuse $user 'pet_2001' $auditSlot $st "seed_audit_$(Get-Random)"
$c = Invoke-Api 'POST' '/api/user/app/cancel' $user @{ appId = $a3; idemKey = "seed_cancel_$(Get-Random)"; reason = 'something urgent, please help cancel' }
if ($c.code -ne 0) { Write-Host "[SKIP] cancel $a3 : $($c.message)" } else { Write-Host "[OK] audit pending $a3" }

# ---------- 4. one refund order (tomorrow slot, >=2h auto accept) ----------
$a4 = Book-Or-Reuse $user 'pet_2002' 'slot_03' '2026-11-17 14:00' "seed_refund_$(Get-Random)"
$c2 = Invoke-Api 'POST' '/api/user/app/cancel' $user @{ appId = $a4; idemKey = "seed_cancel2_$(Get-Random)" }
if ($c2.code -ne 0) { Write-Host "[SKIP] cancel $a4 : $($c2.message)" } else { Write-Host "[OK] refund created for $a4" }

Write-Host 'seed done'
