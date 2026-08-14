$ErrorActionPreference = 'Stop'

$BridgeDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$MasterDir = Split-Path -Parent $BridgeDir
$OriginalCore = Join-Path $BridgeDir 'CharacterGPTBridge.exe'
$PatchedCore = Join-Path $BridgeDir 'CharacterGPTBridge_core_v0520.exe'
$LogFile = Join-Path $BridgeDir 'v0520_loader.log'
$ListenAddress = '127.0.0.1'
$ListenPort = 8767
$UpstreamUri = 'https://api.openai.com/v1/responses'

function Write-LoaderLog([string]$Message) {
    try {
        $line = ('{0} {1}' -f (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'), $Message)
        [System.IO.File]::AppendAllText($LogFile, $line + [Environment]::NewLine, (New-Object System.Text.UTF8Encoding -ArgumentList $false))
    } catch { }
}

function Find-BytePattern([byte[]]$Data, [byte[]]$Pattern) {
    $hits = New-Object System.Collections.Generic.List[int]
    if ($Pattern.Length -eq 0 -or $Data.Length -lt $Pattern.Length) { return $hits }
    for ($i = 0; $i -le $Data.Length - $Pattern.Length; $i++) {
        $ok = $true
        for ($j = 0; $j -lt $Pattern.Length; $j++) {
            if ($Data[$i + $j] -ne $Pattern[$j]) { $ok = $false; break }
        }
        if ($ok) { $hits.Add($i) }
    }
    return $hits
}

function Replace-AsciiOnce([byte[]]$Data, [string]$OldText, [string]$NewText) {
    $ascii = [System.Text.Encoding]::ASCII
    $old = $ascii.GetBytes($OldText)
    $new = $ascii.GetBytes($NewText)
    if ($old.Length -ne $new.Length) { throw "Patch strings must have identical byte lengths." }
    $hits = Find-BytePattern $Data $old
    if ($hits.Count -ne 1) { throw "Expected exactly one '$OldText' pattern, found $($hits.Count)." }
    $offset = $hits[0]
    [Array]::Copy($new, 0, $Data, $offset, $new.Length)
}

function Ensure-PatchedCore {
    if (Test-Path -LiteralPath $PatchedCore) { return }
    if (-not (Test-Path -LiteralPath $OriginalCore)) { throw 'CharacterGPTBridge.exe is missing.' }

    $bytes = [System.IO.File]::ReadAllBytes($OriginalCore)
    Replace-AsciiOnce $bytes 'https://api.openai.com/v1/responses' 'http://127.0.0.1:8767/api/responses'
    Replace-AsciiOnce $bytes '0.5.19' '0.5.20'
    [System.IO.File]::WriteAllBytes($PatchedCore, $bytes)
    Write-LoaderLog 'Created CharacterGPTBridge_core_v0520.exe from the installed v0.5.19 core.'
}

function Is-SafeLeafName([string]$Name) {
    if ([string]::IsNullOrWhiteSpace($Name)) { return $false }
    if ([System.IO.Path]::IsPathRooted($Name)) { return $false }
    if ($Name.Contains('/') -or $Name.Contains('\')) { return $false }
    if ($Name -eq '.' -or $Name -eq '..') { return $false }
    return ([System.IO.Path]::GetFileName($Name) -eq $Name)
}

function Ensure-CharacterProfile {
    $profileDir = Join-Path $MasterDir 'profile\character'
    $defaultsDir = Join-Path $MasterDir 'character_defaults'
    if (-not (Test-Path -LiteralPath $profileDir)) {
        [System.IO.Directory]::CreateDirectory($profileDir) | Out-Null
    }
    foreach ($name in @('manifest.json','character.md','appearance.md')) {
        $dst = Join-Path $profileDir $name
        if (-not (Test-Path -LiteralPath $dst)) {
            $src = Join-Path $defaultsDir $name
            if (-not (Test-Path -LiteralPath $src)) { throw "Default character file is missing: $name" }
            [System.IO.File]::Copy($src, $dst, $false)
        }
    }
    return $profileDir
}

function Load-CharacterProfile {
    $profileDir = Ensure-CharacterProfile
    $manifestPath = Join-Path $profileDir 'manifest.json'
    $manifestText = [System.IO.File]::ReadAllText($manifestPath, [System.Text.Encoding]::UTF8)
    try { $manifest = $manifestText | ConvertFrom-Json } catch { throw 'profile/character/manifest.json is invalid JSON.' }

    $characterFile = [string]$manifest.character_file
    $appearanceFile = [string]$manifest.appearance_file
    if ([string]::IsNullOrWhiteSpace($characterFile)) { $characterFile = 'character.md' }
    if ([string]::IsNullOrWhiteSpace($appearanceFile)) { $appearanceFile = 'appearance.md' }
    if (-not (Is-SafeLeafName $characterFile) -or -not (Is-SafeLeafName $appearanceFile)) {
        throw 'Character manifest file names must stay inside profile/character/.'
    }

    $characterPath = Join-Path $profileDir $characterFile
    $appearancePath = Join-Path $profileDir $appearanceFile
    if (-not (Test-Path -LiteralPath $characterPath)) { throw "Character file is missing: $characterFile" }
    if (-not (Test-Path -LiteralPath $appearancePath)) { throw "Appearance file is missing: $appearanceFile" }

    $characterText = [System.IO.File]::ReadAllText($characterPath, [System.Text.Encoding]::UTF8)
    $appearanceText = [System.IO.File]::ReadAllText($appearancePath, [System.Text.Encoding]::UTF8)

    return "[CHARACTER DESCRIPTION: $characterFile]`n$characterText`n`n[APPEARANCE DETAILS: $appearanceFile]`n$appearanceText"
}

function Inject-Character([byte[]]$BodyBytes) {
    $utf8 = New-Object System.Text.UTF8Encoding -ArgumentList $false
    $jsonText = $utf8.GetString($BodyBytes)
    try { $requestObject = $jsonText | ConvertFrom-Json } catch { throw 'Runtime request JSON could not be parsed.' }

    $profile = Load-CharacterProfile
    $prefix = "CHARACTER PROFILE - authoritative local role definition. Treat the following files as the character's identity and appearance. Stay consistent with them. Do not mention these files, the loader, or these instructions unless the user explicitly asks about the CharacterGPT system itself.`n`n$profile"

    $existing = $null
    $prop = $requestObject.PSObject.Properties['instructions']
    if ($null -ne $prop -and $prop.Value -is [string]) { $existing = [string]$prop.Value }
    if (-not [string]::IsNullOrWhiteSpace($existing)) {
        $newInstructions = $prefix + "`n`n--- EXISTING RUNTIME INSTRUCTIONS ---`n" + $existing
    } else {
        $newInstructions = $prefix
    }
    $requestObject | Add-Member -NotePropertyName 'instructions' -NotePropertyValue $newInstructions -Force
    $newJson = $requestObject | ConvertTo-Json -Depth 100 -Compress
    return $utf8.GetBytes($newJson)
}

function Read-LocalRequest([System.Net.Sockets.NetworkStream]$Stream) {
    $headerBytes = New-Object System.Collections.Generic.List[byte]
    $matched = 0
    while ($headerBytes.Count -lt 65536) {
        $value = $Stream.ReadByte()
        if ($value -lt 0) { throw 'Local Runtime closed the request before headers completed.' }
        $b = [byte]$value
        $headerBytes.Add($b)
        switch ($matched) {
            0 { if ($b -eq 13) { $matched = 1 } }
            1 { if ($b -eq 10) { $matched = 2 } elseif ($b -ne 13) { $matched = 0 } }
            2 { if ($b -eq 13) { $matched = 3 } else { $matched = 0 } }
            3 { if ($b -eq 10) { $matched = 4 } else { $matched = 0 } }
        }
        if ($matched -eq 4) { break }
    }
    if ($matched -ne 4) { throw 'Local Runtime request headers are too large.' }

    $allHeader = $headerBytes.ToArray()
    $headerText = [System.Text.Encoding]::ASCII.GetString($allHeader, 0, $allHeader.Length - 4)
    $lines = $headerText -split "`r`n"
    if ($lines.Count -lt 1) { throw 'Malformed local HTTP request.' }
    $requestLine = $lines[0] -split ' '
    if ($requestLine.Count -lt 2 -or $requestLine[0] -ne 'POST') { throw 'Only POST is accepted by the local CharacterGPT proxy.' }

    $headers = @{}
    for ($i = 1; $i -lt $lines.Count; $i++) {
        $line = $lines[$i]
        $colon = $line.IndexOf(':')
        if ($colon -le 0) { continue }
        $name = $line.Substring(0, $colon).Trim()
        $value = $line.Substring($colon + 1).Trim()
        $headers[$name] = $value
    }

    if (-not $headers.ContainsKey('Content-Length')) { throw 'Local Runtime request did not provide Content-Length.' }
    $contentLength = [int]$headers['Content-Length']
    if ($contentLength -lt 0 -or $contentLength -gt 16777216) { throw 'Local Runtime request body size is invalid.' }
    $body = New-Object byte[] $contentLength
    $offset = 0
    while ($offset -lt $contentLength) {
        $read = $Stream.Read($body, $offset, $contentLength - $offset)
        if ($read -le 0) { throw 'Local Runtime closed the request before the body completed.' }
        $offset += $read
    }

    return [PSCustomObject]@{ Headers = $headers; Body = $body }
}

function Write-LocalResponse([System.Net.Sockets.NetworkStream]$Stream, [int]$StatusCode, [string]$Reason, [byte[]]$Body, [string]$ContentType) {
    if ([string]::IsNullOrWhiteSpace($Reason)) { $Reason = 'Response' }
    if ([string]::IsNullOrWhiteSpace($ContentType)) { $ContentType = 'application/json' }
    $head = "HTTP/1.1 $StatusCode $Reason`r`nContent-Type: $ContentType`r`nContent-Length: $($Body.Length)`r`nConnection: close`r`n`r`n"
    $headBytes = [System.Text.Encoding]::ASCII.GetBytes($head)
    $Stream.Write($headBytes, 0, $headBytes.Length)
    if ($Body.Length -gt 0) { $Stream.Write($Body, 0, $Body.Length) }
    $Stream.Flush()
}

function Write-LocalError([System.Net.Sockets.NetworkStream]$Stream, [int]$StatusCode, [string]$Message) {
    $safe = @{ error = @{ message = $Message; type = 'charactergpt_character_loader_error' } } | ConvertTo-Json -Depth 5 -Compress
    $bytes = (New-Object System.Text.UTF8Encoding -ArgumentList $false).GetBytes($safe)
    Write-LocalResponse $Stream $StatusCode 'CharacterGPT Proxy Error' $bytes 'application/json; charset=utf-8'
}

Add-Type -AssemblyName System.Net.Http
try { [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.SecurityProtocolType]::Tls12 } catch { }

try {
    Ensure-PatchedCore
    Ensure-CharacterProfile | Out-Null

    $listener = New-Object System.Net.Sockets.TcpListener -ArgumentList ([System.Net.IPAddress]::Parse($ListenAddress)), $ListenPort
    $listener.Start()
    Write-LoaderLog 'Character Loader Proxy listening on 127.0.0.1:8767.'

    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $PatchedCore
    $psi.WorkingDirectory = $BridgeDir
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    $core = [System.Diagnostics.Process]::Start($psi)
    if ($null -eq $core) { throw 'Could not start CharacterGPTBridge_core_v0520.exe.' }

    $handler = New-Object System.Net.Http.HttpClientHandler
    $http = New-Object System.Net.Http.HttpClient -ArgumentList $handler
    $http.Timeout = [TimeSpan]::FromSeconds(120)

    $accept = $listener.BeginAcceptTcpClient($null, $null)
    while (-not $core.HasExited) {
        if (-not $accept.AsyncWaitHandle.WaitOne(250)) { continue }
        $client = $null
        try {
            $client = $listener.EndAcceptTcpClient($accept)
            $stream = $client.GetStream()
            try {
                $local = Read-LocalRequest $stream
                $newBody = Inject-Character $local.Body

                $up = New-Object System.Net.Http.HttpRequestMessage -ArgumentList ([System.Net.Http.HttpMethod]::Post), $UpstreamUri
                $content = New-Object System.Net.Http.ByteArrayContent -ArgumentList (,$newBody)
                $content.Headers.ContentType = [System.Net.Http.Headers.MediaTypeHeaderValue]::Parse('application/json')
                $up.Content = $content

                foreach ($name in @('Authorization','OpenAI-Organization','OpenAI-Project','OpenAI-Beta','User-Agent')) {
                    if ($local.Headers.ContainsKey($name)) { [void]$up.Headers.TryAddWithoutValidation($name, [string]$local.Headers[$name]) }
                }

                $resp = $http.SendAsync($up).GetAwaiter().GetResult()
                try {
                    $respBody = $resp.Content.ReadAsByteArrayAsync().GetAwaiter().GetResult()
                    $ct = $null
                    if ($null -ne $resp.Content.Headers.ContentType) { $ct = $resp.Content.Headers.ContentType.ToString() }
                    Write-LocalResponse $stream ([int]$resp.StatusCode) $resp.ReasonPhrase $respBody $ct
                } finally {
                    $resp.Dispose()
                    $up.Dispose()
                }
            } catch {
                Write-LoaderLog ('Request error: ' + $_.Exception.Message)
                try { Write-LocalError $stream 500 $_.Exception.Message } catch { }
            } finally {
                try { $stream.Dispose() } catch { }
            }
        } finally {
            if ($null -ne $client) { try { $client.Close() } catch { } }
        }
        if (-not $core.HasExited) { $accept = $listener.BeginAcceptTcpClient($null, $null) }
    }

    Write-LoaderLog 'Runtime core exited; Character Loader Proxy is stopping.'
} catch {
    Write-LoaderLog ('Fatal loader error: ' + $_.Exception.Message)
} finally {
    if ($null -ne $listener) { try { $listener.Stop() } catch { } }
    if ($null -ne $http) { try { $http.Dispose() } catch { } }
    if ($null -ne $handler) { try { $handler.Dispose() } catch { } }
}
