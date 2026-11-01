# Omnoda Windows user installer.
# irm https://HOST/install.ps1 | iex; Install-Omnoda -Server https://HOST -Code CODE
$ErrorActionPreference = "Stop"

# Trust anchor. Same key as scripts/install.sh. Not a SHA256 sidecar.
$Script:OmnodaReleasePub = "/lAwmdAmddtlLwrmLv3P3+c82yUaMuYeJO7FW4LQW/k="

function Install-Omnoda {
  param(
    [Parameter(Mandatory=$true)][string]$Server,
    [Parameter(Mandatory=$true)][string]$Code,
    [string]$Name = "",
    [string]$Workspace = "",
    [string]$Label = ""
  )
  $version = "0.1.0"
  $arch = "amd64"
  if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { $arch = "arm64" }
  $asset = "omnoda_${version}_windows_${arch}.exe"
  $base = $Server.TrimEnd("/")
  $dir = Join-Path $env:LOCALAPPDATA "Omnoda\bin"
  New-Item -ItemType Directory -Force -Path $dir | Out-Null
  $dest = Join-Path $dir "omnoda.exe"
  $tmp = Join-Path $env:TEMP ("omnoda-" + [guid]::NewGuid().ToString())
  New-Item -ItemType Directory -Force -Path $tmp | Out-Null
  $bin = Join-Path $tmp "omnoda.exe"
  $sig = Join-Path $tmp "omnoda.sig"
  $ok = $false
  foreach ($i in 1..3) {
    try {
      Invoke-WebRequest -Uri "$base/releases/$asset" -OutFile $bin
      Invoke-WebRequest -Uri "$base/releases/$asset.sig" -OutFile $sig
      $ok = $true
      break
    } catch {
      Start-Sleep -Seconds (2 * $i)
    }
  }
  if (-not $ok) { throw "download failed" }
  if ($Script:OmnodaReleasePub -eq "/lAwmdAmddtlLwrmLv3P3+c82yUaMuYeJO7FW4LQW/k=") {
    throw "installer has no release trust anchor"
  }
  # Verify with .NET. Do not start the downloaded exe before this returns.
  Add-Type -AssemblyName System.Security
  $pub = [Convert]::FromBase64String($Script:OmnodaReleasePub)
  $sigBytes = [Convert]::FromBase64String((Get-Content $sig -Raw).Trim())
  $data = [IO.File]::ReadAllBytes($bin)
  # Ed25519 is available as a NSec-free path only on newer .NET. We shell out to
  # the previously installed omnoda-verify when present, else use OpenSSL if installed.
  $verified = $false
  if (Get-Command openssl -ErrorAction SilentlyContinue) {
    $pem = Join-Path $tmp "pub.pem"
    $prefix = [byte[]](0x30,0x2a,0x30,0x05,0x06,0x03,0x2b,0x65,0x70,0x03,0x21,0x00)
    $der = New-Object byte[] ($prefix.Length + $pub.Length)
    [Buffer]::BlockCopy($prefix, 0, $der, 0, $prefix.Length)
    [Buffer]::BlockCopy($pub, 0, $der, $prefix.Length, $pub.Length)
    $b64 = [Convert]::ToBase64String($der, "InsertLineBreaks")
    Set-Content -Path $pem -Value "-----BEGIN PUBLIC KEY-----`n$b64`n-----END PUBLIC KEY-----"
    [IO.File]::WriteAllBytes((Join-Path $tmp "sig.bin"), $sigBytes)
    & openssl pkeyutl -verify -pubin -inkey $pem -rawin -in $bin -sigfile (Join-Path $tmp "sig.bin")
    if ($LASTEXITCODE -eq 0) { $verified = $true }
  }
  if (-not $verified) { throw "signature verification failed; install OpenSSL or use a signed verifier" }
  if (Test-Path $dest) { Copy-Item $dest "$dest.prev" -Force }
  Copy-Item $bin $dest -Force
  $args = @("join", "--server", $Server, "--code", $Code)
  if ($Name) { $args += @("--name", $Name) }
  if ($Workspace) { $args += @("--workspace", $Workspace) }
  if ($Label) { $args += @("--label", $Label) }
  & $dest @args
  if ($LASTEXITCODE -ne 0) {
    if (Test-Path "$dest.prev") { Copy-Item "$dest.prev" $dest -Force }
    throw "join failed"
  }
  # User logon task. This is not a SYSTEM service. An admin service is a separate explicit step.
  $action = New-ScheduledTaskAction -Execute $dest -Argument "start"
  $trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
  Register-ScheduledTask -TaskName "OmnodaNode" -Action $action -Trigger $trigger -Force | Out-Null
  Start-ScheduledTask -TaskName "OmnodaNode"
  Write-Output "Omnoda installed for user $env:USERNAME"
}
