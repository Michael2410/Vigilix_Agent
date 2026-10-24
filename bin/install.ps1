# ==============================================================================
# Vigilix Agent - Instalador para Windows (PowerShell)
# Uso (como Administrador): .\install.ps1 [-Server "http://<IP>/api"] [-Token "<TOKEN>"]
# ==============================================================================
param (
    [string]$Server = "",
    [string]$Token = ""
)

$ErrorActionPreference = "Stop"

# 1. Comprobar permisos de administrador
$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    Write-Warning "⚠️ Por favor ejecuta PowerShell como Administrador para poder instalar el servicio."
}

# 2. Solicitar Token si no fue provisto
if (-not $Token) {
    Write-Host ""
    Write-Host "==========================================" -ForegroundColor Cyan
    Write-Host "🔑 Configuración de Enrolamiento de Vigilix" -ForegroundColor Cyan
    Write-Host "==========================================" -ForegroundColor Cyan
    $Token = Read-Host "👉 Ingresa el TOKEN generado en la web"
    if (-not $Token) {
        Write-Error "El token no puede estar vacío."
        exit 1
    }
}

# 3. Solicitar Server URL si no fue provisto
if (-not $Server) {
    $DefaultUrl = "http://localhost/api"
    $inputUrl = Read-Host "🌐 URL del servidor Vigilix [Enter para usar '$DefaultUrl']"
    if ($inputUrl) {
        $Server = $inputUrl
    } else {
        $Server = $DefaultUrl
    }
}

$InstallDir = "C:\Program Files\VigilixAgent"
if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

$BinarySource = Join-Path $PSScriptRoot "vigilix-agent-windows.exe"
if (-not (Test-Path $BinarySource)) {
    $BinarySource = ".\vigilix-agent-windows.exe"
}

if (-not (Test-Path $BinarySource)) {
    Write-Error "No se encontró el ejecutable vigilix-agent-windows.exe en la carpeta actual."
    exit 1
}

$DestExe = Join-Path $InstallDir "vigilix-agent.exe"
$DestConfig = Join-Path $InstallDir "config.yaml"

Write-Host "📦 Copiando ejecutable a $DestExe..." -ForegroundColor Yellow
Copy-Item -Path $BinarySource -Destination $DestExe -Force

# 4. Enrolamiento
Write-Host "🔑 Enrolando agente contra $Server..." -ForegroundColor Green
& $DestExe -enroll -server "$Server" -token "$Token" -config "$DestConfig"

if (-not (Test-Path $DestConfig)) {
    Write-Error "No se pudo generar $DestConfig. Revisa la conectividad o el token."
    exit 1
}

# 5. Registrar tarea en segundo plano que arranca con Windows
Write-Host "⚙️ Registrando tarea en segundo plano (VigilixAgent)..." -ForegroundColor Yellow

$Action = New-ScheduledTaskAction -Execute $DestExe -Argument "-config `"$DestConfig`""
$Trigger = New-ScheduledTaskTrigger -AtStartup
$Principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest
$Settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1)

Unregister-ScheduledTask -TaskName "VigilixAgent" -Confirm:$false -ErrorAction SilentlyContinue | Out-Null
Register-ScheduledTask -TaskName "VigilixAgent" -Action $Action -Trigger $Trigger -Principal $Principal -Settings $Settings -Description "Vigilix Monitoring Agent" | Out-Null
Start-ScheduledTask -TaskName "VigilixAgent"

Write-Host ""
Write-Host "==========================================" -ForegroundColor Green
Write-Host "✅ ¡Vigilix Agent instalado con éxito!" -ForegroundColor Green
Write-Host "==========================================" -ForegroundColor Green
Write-Host "El agente se encuentra activo y programado para iniciar con Windows."
Write-Host "• Ubicación: $InstallDir"
Write-Host "• Ver tarea: Get-ScheduledTask VigilixAgent"
Write-Host "• Iniciar:   Start-ScheduledTask VigilixAgent"
Write-Host "• Detener:   Stop-ScheduledTask VigilixAgent"
