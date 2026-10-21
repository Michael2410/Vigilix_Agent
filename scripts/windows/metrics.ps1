param(
    [string[]]$Services = @()
)

$ErrorActionPreference = 'SilentlyContinue'

$cpu = Get-CimInstance Win32_PerfFormattedData_PerfOS_Processor -Filter "Name='_Total'" | Select-Object -Property PercentProcessorTime
$os = Get-CimInstance Win32_OperatingSystem | Select-Object -Property TotalVisibleMemorySize, FreePhysicalMemory, LastBootUpTime, Caption, Version
$cs = Get-CimInstance Win32_ComputerSystem | Select-Object -Property NumberOfLogicalProcessors
$proc = Get-CimInstance Win32_Processor | Select-Object -Property Name -First 1
$disks = Get-CimInstance Win32_LogicalDisk -Filter "DriveType=3" | Select-Object -Property DeviceID, Size, FreeSpace
$net = Get-CimInstance Win32_PerfFormattedData_Tcpip_NetworkInterface | Select-Object -Property Name, BytesReceivedPersec, BytesSentPersec

$svcList = @()
$svcNames = @()
foreach ($item in $Services) {
    if ($item) {
        $svcNames += ($item -split '[,; ]') | Where-Object { $_ -ne '' }
    }
}

foreach ($s in $svcNames) {
    $svc = Get-Service -Name $s -ErrorAction SilentlyContinue
    if ($svc) {
        $status = if ($svc.Status -eq 'Running') { 'running' } elseif ($svc.Status -eq 'Stopped') { 'stopped' } else { 'unknown' }
        $svcList += @{ name = $s; status = $status }
    } else {
        $svcList += @{ name = $s; status = 'stopped' }
    }
}

$bootTimeUtc = $null
if ($os.LastBootUpTime) {
    if ($os.LastBootUpTime -is [datetime]) {
        $bootTimeUtc = $os.LastBootUpTime.ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
    } else {
        try {
            $bootTimeUtc = [System.Management.ManagementDateTimeConverter]::ToDateTime($os.LastBootUpTime).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
        } catch {}
    }
}

$result = @{
    cpu_usage_pct = [double]$cpu.PercentProcessorTime
    mem_total_kb = [int64]$os.TotalVisibleMemorySize
    mem_free_kb = [int64]$os.FreePhysicalMemory
    os_name = [string]$os.Caption
    os_version = [string]$os.Version
    cpu_model = [string]$proc.Name
    cpu_cores = [int]$cs.NumberOfLogicalProcessors
    boot_time = $bootTimeUtc
    disks = @($disks | ForEach-Object {
        @{
            device = [string]$_.DeviceID
            size_bytes = [int64]$_.Size
            free_bytes = [int64]$_.FreeSpace
        }
    })
    interfaces = @($net | ForEach-Object {
        @{
            name = [string]$_.Name
            rx_bps = [int64]$_.BytesReceivedPersec
            tx_bps = [int64]$_.BytesSentPersec
        }
    })
    services = $svcList
}

$result | ConvertTo-Json -Compress -Depth 4
