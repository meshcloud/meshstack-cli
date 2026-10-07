# Installs meshstack on Windows: with winget when it can, or else from the GitHub releases of
# meshcloud/meshstack-cli.
#
#   irm https://raw.githubusercontent.com/meshcloud/meshstack-cli/main/install.ps1 | iex
#   & ([scriptblock]::Create((irm https://raw.githubusercontent.com/meshcloud/meshstack-cli/main/install.ps1))) -Version v0.3.0 -Dir ~\bin
#
# This is the counterpart of install.sh and keeps its options and its messages. It runs on Windows
# PowerShell 5.1 and on PowerShell 7.
#
# The archive and checksum names follow name_template in .goreleaser.yml and must stay in step
# with it, and the winget package identifier follows winget.package_identifier there.

[CmdletBinding()]
param(
    [string]$Version = $env:MESHSTACK_INSTALL_VERSION,
    [string]$Dir = $env:MESHSTACK_INSTALL_DIR,
    [switch]$NoWinget,
    [switch]$Help
)

# `irm | iex` runs this script in the scope of the user's own session. Everything below runs in a
# child scope, so its functions and preference variables do not stay behind in that session.
& {
    $ErrorActionPreference = 'Stop'
    # Windows PowerShell 5.1 shows a progress bar that makes downloads many times slower.
    $ProgressPreference = 'SilentlyContinue'

    $Repo = 'meshcloud/meshstack-cli'
    $Project = 'meshstack-cli'
    $Exe = 'meshstack.exe'
    $WingetId = 'meshcloud.meshstack-cli'

    function Write-Usage {
        Write-Host @"
Usage: install.ps1 [-Version vX.Y.Z] [-Dir DIRECTORY] [-NoWinget]

Installs meshstack with winget, when winget is available and neither -Version nor -Dir
is given. Otherwise downloads the meshstack binary from https://github.com/$Repo/releases,
checks its SHA-256 checksum and installs it.

  -Version   The release to install. Defaults to the latest release.
  -Dir       The directory to install into. Defaults to the one of the meshstack
             on PATH, or to %LOCALAPPDATA%\Programs\$Project. The script adds
             the directory to your user PATH.
  -NoWinget  Download from GitHub even when winget is available.

The environment variables MESHSTACK_INSTALL_VERSION and MESHSTACK_INSTALL_DIR set the
same values as the parameters.

To pass parameters to the script that irm downloads, run it as a script block:

  & ([scriptblock]::Create((irm https://raw.githubusercontent.com/$Repo/main/install.ps1))) -Version v1.2.3
"@
    }

    function Write-Warn([string]$Message) {
        [Console]::Error.WriteLine("warning: $Message")
    }

    function Invoke-Download([string]$Url, [string]$OutFile) {
        for ($attempt = 1; ; $attempt++) {
            try {
                Invoke-WebRequest -Uri $Url -OutFile $OutFile -UseBasicParsing
                return
            } catch {
                if ($attempt -ge 3) {
                    throw "could not download ${Url}: $($_.Exception.Message)"
                }
                Start-Sleep -Seconds $attempt
            }
        }
    }

    # GitHub answers /releases/latest with a redirect to /releases/tag/<tag>. Following it avoids
    # the REST API and its rate limit, as install.sh does.
    function Get-LatestVersion {
        $url = "https://github.com/$Repo/releases/latest"
        try {
            $response = Invoke-WebRequest -Uri $url -Method Head -UseBasicParsing
        } catch {
            throw "could not reach $url"
        }
        # Windows PowerShell 5.1 and PowerShell 7 keep the final URL in different places.
        $final = $response.BaseResponse.ResponseUri
        if (-not $final) {
            $final = $response.BaseResponse.RequestMessage.RequestUri
        }
        $tag = ([string]$final).TrimEnd('/').Split('/')[-1]
        if ($tag -notmatch '^v[0-9]') {
            throw "could not read the latest release from '$final'; pass -Version vX.Y.Z"
        }
        return $tag
    }

    function Get-Arch {
        # A 32-bit PowerShell on 64-bit Windows sees x86 in PROCESSOR_ARCHITECTURE, and the real
        # architecture in PROCESSOR_ARCHITEW6432.
        $name = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
        switch ($name) {
            'AMD64' { return 'amd64' }
            # No windows/arm64 release exists, and Windows on ARM runs the amd64 binary under emulation.
            'ARM64' { return 'amd64' }
            default { throw "no meshstack release exists for the CPU architecture '$name'" }
        }
    }

    function Test-Checksum([string]$File, [string]$Name, [string]$SumsFile) {
        $expected = $null
        foreach ($line in Get-Content -LiteralPath $SumsFile) {
            if ($line -match '^([0-9a-fA-F]{64}) [ *]?(.+)$' -and $Matches[2] -eq $Name) {
                $expected = $Matches[1].ToLowerInvariant()
            }
        }
        if (-not $expected) {
            throw "the checksum file has no entry for $Name"
        }
        $actual = (Get-FileHash -LiteralPath $File -Algorithm SHA256).Hash.ToLowerInvariant()
        if ($actual -ne $expected) {
            throw "the checksum of $Name is $actual, but the release says $expected"
        }
    }

    function ConvertTo-DirKey([string]$Path) {
        return $Path.TrimEnd('\', '/').ToLowerInvariant()
    }

    function Test-OnPath([string]$Path, [string]$PathList) {
        $key = ConvertTo-DirKey $Path
        foreach ($entry in ($PathList -split ';')) {
            if ($entry -and (ConvertTo-DirKey ([Environment]::ExpandEnvironmentVariables($entry))) -eq $key) {
                return $true
            }
        }
        return $false
    }

    # Another tool owns these directories and may remove a file it did not put there.
    function Test-ManagedDir([string]$Path) {
        return $Path -match '[\\/](WinGet|scoop|chocolatey)[\\/]'
    }

    function Find-Meshstack {
        $found = Get-Command $Exe -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($found) {
            return $found.Source
        }
        return $null
    }

    # Returns whether winget installed or upgraded meshstack; on $false the caller downloads it
    # from GitHub instead.
    function Install-WithWinget {
        Write-Host "Installing meshstack with winget"
        # Out-Host keeps the output of winget out of the return value of this function.
        & winget install --exact --id $WingetId --source winget --accept-source-agreements --accept-package-agreements | Out-Host
        $code = '0x{0:X8}' -f $LASTEXITCODE
        switch ($code) {
            '0x00000000' { return $true }
            # APPINSTALLER_CLI_ERROR_UPDATE_NOT_APPLICABLE and _PACKAGE_ALREADY_INSTALLED: meshstack is
            # installed, and winget has no newer version. The codes are listed in
            # https://github.com/microsoft/winget-cli/blob/master/doc/windows/package-manager/winget/returnCodes.md
            '0x8A15002B' { return $true }
            '0x8A150061' { return $true }
            '0x8A150014' { Write-Warn "winget does not know the package $WingetId yet; downloading meshstack from GitHub instead" }
            default { Write-Warn "winget failed with exit code $code; downloading meshstack from GitHub instead" }
        }
        return $false
    }

    # Writes the registry value itself, because [Environment]::SetEnvironmentVariable would store
    # the user PATH expanded, which replaces entries such as %USERPROFILE%\bin with fixed paths.
    function Add-UserPath([string]$Path) {
        $key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
        try {
            $userPath = $key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
            $newPath = if ($userPath) { "$($userPath.TrimEnd(';'));$Path" } else { $Path }
            $key.SetValue('Path', $newPath, [Microsoft.Win32.RegistryValueKind]::ExpandString)
        } finally {
            $key.Close()
        }
        # Setting a user variable through .NET broadcasts WM_SETTINGCHANGE, so that Explorer, and
        # every terminal it starts from now on, reads the new PATH. The variable itself is thrown away.
        [Environment]::SetEnvironmentVariable('MESHSTACK_INSTALL_PATH_CHANGED', '1', 'User')
        [Environment]::SetEnvironmentVariable('MESHSTACK_INSTALL_PATH_CHANGED', $null, 'User')
    }

    function Install-Meshstack {
        if ($Help) {
            Write-Usage
            return
        }

        # $IsWindows exists from PowerShell 6 on; Windows PowerShell 5.1 runs only on Windows.
        if ($PSVersionTable.PSEdition -eq 'Core' -and -not $IsWindows) {
            throw 'install.ps1 installs meshstack on Windows; on Linux and macOS use install.sh'
        }

        # Windows PowerShell 5.1 on older Windows offers only TLS 1.0 and 1.1, which GitHub refuses.
        [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

        $installed = Find-Meshstack
        # An upgrade replaces the meshstack that PATH finds, rather than installing a second one
        # behind it. So winget runs only when no meshstack is on PATH, or when winget installed it.
        # -Version and -Dir skip winget: winget chooses its own directory, and an older version
        # that winget installs is replaced by the next `winget upgrade --all`.
        $installedByWinget = $installed -and $installed -match '[\\/]WinGet[\\/]'
        $wingetAvailable = [bool](Get-Command winget -CommandType Application -ErrorAction SilentlyContinue)
        if (-not $NoWinget -and -not $Version -and -not $Dir -and $wingetAvailable -and (-not $installed -or $installedByWinget)) {
            if (Install-WithWinget) {
                return
            }
        }

        $arch = Get-Arch
        if (-not $Version) {
            $Version = Get-LatestVersion
        }
        if (-not $Version.StartsWith('v')) {
            $Version = "v$Version"
        }
        $number = $Version.Substring(1)
        $archive = "${Project}_${number}_windows_$arch.zip"
        $checksums = "${Project}_${number}_SHA256SUMS"
        $baseUrl = "https://github.com/$Repo/releases/download/$Version"

        $tmp = Join-Path ([IO.Path]::GetTempPath()) "meshstack-install-$([Guid]::NewGuid())"
        New-Item -ItemType Directory -Path $tmp | Out-Null
        try {
            Write-Host "Downloading meshstack $Version for windows/$arch"
            Invoke-Download "$baseUrl/$archive" (Join-Path $tmp $archive)
            Invoke-Download "$baseUrl/$checksums" (Join-Path $tmp $checksums)
            Test-Checksum (Join-Path $tmp $archive) $archive (Join-Path $tmp $checksums)

            $extracted = Join-Path $tmp 'extracted'
            Expand-Archive -LiteralPath (Join-Path $tmp $archive) -DestinationPath $extracted
            $source = Join-Path $extracted $Exe
            if (-not (Test-Path -LiteralPath $source -PathType Leaf)) {
                throw "the archive $archive holds no $Exe"
            }

            if (-not $Dir) {
                if ($installed -and -not (Test-ManagedDir $installed)) {
                    $Dir = Split-Path -Parent $installed
                } else {
                    $Dir = Join-Path $env:LOCALAPPDATA "Programs\$Project"
                }
            }
            $Dir = [IO.Path]::GetFullPath($ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Dir))
            New-Item -ItemType Directory -Path $Dir -Force | Out-Null

            # Windows refuses to overwrite or delete a running .exe, but lets it be renamed. So the
            # old binary moves aside first, and is removed when nothing runs it any more.
            $target = Join-Path $Dir $Exe
            $old = "$target.old"
            Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue
            $staged = Join-Path $Dir ".$Exe.$PID"
            try {
                Copy-Item -LiteralPath $source -Destination $staged -Force
                if (Test-Path -LiteralPath $target) {
                    Move-Item -LiteralPath $target -Destination $old -Force
                }
                Move-Item -LiteralPath $staged -Destination $target
            } catch {
                Remove-Item -LiteralPath $staged -Force -ErrorAction SilentlyContinue
                throw "could not install ${target}: $($_.Exception.Message); pass another -Dir"
            }
            Remove-Item -LiteralPath $old -Force -ErrorAction SilentlyContinue
        } finally {
            Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
        }

        $reported = $null
        try {
            $reported = & $target --version 2>$null | Select-Object -First 1
        } catch {
        }
        if (-not $reported) {
            $reported = "meshstack version $Version"
        }
        Write-Host "Installed $reported to $target"

        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        $machinePath = [Environment]::GetEnvironmentVariable('Path', 'Machine')
        if (-not (Test-OnPath $Dir $userPath) -and -not (Test-OnPath $Dir $machinePath)) {
            Add-UserPath $Dir
            Write-Host ""
            Write-Host "Added $Dir to your user PATH. Open a new terminal, so that it finds meshstack."
        }
        if (-not (Test-OnPath $Dir $env:Path)) {
            $env:Path = "$Dir;$env:Path"
        }

        $found = Find-Meshstack
        if ($found -and (ConvertTo-DirKey $found) -ne (ConvertTo-DirKey $target)) {
            Write-Warn "$found comes first on your PATH, so 'meshstack' still runs that one"
        }
    }

    try {
        Install-Meshstack
    } catch {
        [Console]::Error.WriteLine("error: $($_.Exception.Message)")
        # `exit` would also close the user's session when it runs the script through `irm | iex`,
        # which leaves no $PSCommandPath. Run as a file, the script fails with exit code 1.
        if ($PSCommandPath) {
            exit 1
        }
    }
}
