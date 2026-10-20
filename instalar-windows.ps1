# Instala o LANTV neste PC: abre a porta no firewall, impede o Windows de
# dormir e faz o LANTV iniciar sozinho (e reiniciar se cair).
# Como usar: clique com o botão direito neste arquivo > "Executar com o PowerShell"
# (ou abra o PowerShell como Administrador). A pasta precisa ter lantv.exe,
# ffmpeg.exe e ffprobe.exe.

#Requires -RunAsAdministrator
$ErrorActionPreference = 'Stop'
$pasta = Split-Path -Parent $MyInvocation.MyCommand.Path

foreach ($f in 'lantv.exe', 'ffmpeg.exe', 'ffprobe.exe') {
    if (-not (Test-Path (Join-Path $pasta $f))) { throw "Falta o arquivo $f na pasta $pasta" }
}

Remove-NetFirewallRule -DisplayName 'LANTV' -ErrorAction SilentlyContinue
New-NetFirewallRule -DisplayName 'LANTV' -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow -Profile Any | Out-Null

powercfg /change standby-timeout-ac 0
powercfg /change hibernate-timeout-ac 0
powercfg /change monitor-timeout-ac 0

$acao = New-ScheduledTaskAction -Execute (Join-Path $pasta 'lantv.exe') -WorkingDirectory $pasta
$gatilho = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
$ajustes = New-ScheduledTaskSettingsSet -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) `
    -ExecutionTimeLimit ([TimeSpan]::Zero) -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
Register-ScheduledTask -TaskName 'LANTV' -Action $acao -Trigger $gatilho -Settings $ajustes -RunLevel Highest -Force | Out-Null
Stop-ScheduledTask -TaskName 'LANTV' -ErrorAction SilentlyContinue
Start-ScheduledTask -TaskName 'LANTV'

$ip = (Get-NetIPAddress -AddressFamily IPv4 | Where-Object { $_.IPAddress -notlike '127.*' -and $_.IPAddress -notlike '169.*' } | Select-Object -First 1).IPAddress
Write-Host ""
Write-Host "LANTV instalado. Painel: http://${ip}:8080/"
Write-Host "Para o PC ligar sozinho e abrir o LANTV após queda de energia: configure o login automático do Windows (netplwiz) e, na BIOS, 'Restore on AC Power Loss = Power On'."
