# Install wsl-spawner from a GitHub release, for the current user.
#
#   irm https://github.com/kanggu12/wsl-spawner/releases/latest/download/install.ps1 | iex
#
# Settings, all optional, as environment variables:
#   WSL_SPAWNER_VERSION   release to install, like v0.1.0 (default: the latest)
#   WSL_SPAWNER_BIN_DIR   where it goes (default: %LOCALAPPDATA%\Programs\wsl-spawner)
#   WSL_SPAWNER_BASE_URL  where releases are downloaded from (mirrors, tests)
#   WSL_SPAWNER_ZIP       a zip already downloaded, installed as it is (tests)
#   WSL_SPAWNER_NO_SETUP  1: do not open setup after a first install
#   WSL_SPAWNER_UNINSTALL 1: remove wsl-spawner instead, with what it put on this
#                          PC (its own uninstall: the sign-out hook, the
#                          sign-in program, what it set up in the running
#                          distributions), the program and its PATH entry;
#                          the settings and history stay unless
#   WSL_SPAWNER_PURGE     is 1 too
#
# Everything runs in a script block of its own: run through iex, the script
# shares the caller's session, and a failure must end the install, not the
# person's PowerShell window, nor leave settings like ErrorActionPreference
# behind. A failure is said in one red line in a window, and thrown to a
# script that runs this, for its exit code.
& {
  $ErrorActionPreference = 'Stop'
  # A person types the install into a PowerShell window; a script runs it
  # in one that ends with it (powershell -Command, -File, -EncodedCommand),
  # unless told to stay (-NoExit).
  $argv = @([Environment]::GetCommandLineArgs() | Select-Object -Skip 1)
  $oneShot = $argv | Where-Object { $_ -match '^[-/](c|co|com|comm|comma|comman|command|cwa|commandwithargs|f|fi|fil|file|e|ec|en|enc|enco|encod|encode|encoded|encodedc|encodedco|encodedcom|encodedcomm|encodedcomma|encodedcomman|encodedcommand)$' }
  $window = ($argv -match '^[-/]noe') -or -not $oneShot
  try {
    # Windows PowerShell 5.1 speaks only older TLS unless told otherwise.
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    $repo = 'kanggu12/wsl-spawner'
    $base = if ($env:WSL_SPAWNER_BASE_URL) { $env:WSL_SPAWNER_BASE_URL } else { "https://github.com/$repo/releases/download" }
    $binDir = if ($env:WSL_SPAWNER_BIN_DIR) { $env:WSL_SPAWNER_BIN_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\wsl-spawner' }
    # Releases come over https only: over http, with their own checksums, a
    # download could be anything. The folder is a full path: a relative one
    # would go on the PATH as written, a folder under whatever directory a
    # terminal happens to be in.
    if ($base -notmatch '^https://') { throw 'wsl-spawner install: WSL_SPAWNER_BASE_URL must be an https:// address' }
    if ($binDir -notmatch '^([A-Za-z]:[\\/]|\\\\[^\\/])') { throw 'wsl-spawner install: WSL_SPAWNER_BIN_DIR must be a full path, like C:\Program Files\wsl-spawner' }
    # The program's three files, in the folder and in each zip: the
    # program, the same built with no console (the sign-in program and the
    # sign-out hook start it, so no window opens), and the WSL launcher.
    $files = 'wsl-spawner.exe', 'wsl-spawnerw.exe', 'wsl-spawner'

    # The user's PATH as it is stored, so entries like %USERPROFILE%\bin keep
    # their variables when it is written back, its empty entries stay, and
    # its type stays: expandable, or plain as some tools store it. Whether
    # an entry is the program's folder is told however it was written: its
    # variables expanded, its quotes and a trailing backslash dropped, in
    # any case, as the program's own uninstall tells it.
    $pathKey = { [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true) }
    $isBinDir = { param($entry) [Environment]::ExpandEnvironmentVariables($entry).Trim().Trim('"').TrimEnd('\', '/') -eq $binDir.Trim().Trim('"').TrimEnd('\', '/') }
    $pathKind = {
      param($key)
      # A PATH not there yet is made expandable, as Windows makes it.
      $kind = try { $key.GetValueKind('Path') } catch { [Microsoft.Win32.RegistryValueKind]::ExpandString }
      if ($kind -eq [Microsoft.Win32.RegistryValueKind]::String) { $kind } else { [Microsoft.Win32.RegistryValueKind]::ExpandString }
    }
    # The PATH value with the folder added last, or $null when it is there:
    # the value is written on as it was, a semicolon put in only between.
    $withBinDir = {
      param($value)
      if (@($value -split ';' | Where-Object { & $isBinDir $_ }).Count) { $null }
      elseif ($value -eq '') { $binDir }
      elseif ($value.EndsWith(';')) { $value + $binDir }
      else { $value + ';' + $binDir }
    }
    # The PATH value without the folder's entries, or $null when it has
    # none: every other entry is kept as it is written, empty ones too.
    $withoutBinDir = {
      param($value)
      $entries = @($value -split ';')
      $kept = @($entries | Where-Object { -not (& $isBinDir $_) })
      if ($kept.Count -eq $entries.Count) { $null } else { $kept -join ';' }
    }
    # Tell Windows the environment changed, so new terminals see it: setting a
    # variable through .NET sends that message.
    $announce = {
      [Environment]::SetEnvironmentVariable('WSL_SPAWNER_INSTALLING', '1', 'User')
      [Environment]::SetEnvironmentVariable('WSL_SPAWNER_INSTALLING', $null, 'User')
    }

    if ($env:WSL_SPAWNER_UNINSTALL -eq '1') {
      # The two are one-shot, whichever way this ends: left set, the install
      # an error here says to run in this window would remove again what it
      # installs.
      $purgeAsked = $env:WSL_SPAWNER_PURGE -eq '1'
      Remove-Item Env:WSL_SPAWNER_UNINSTALL, Env:WSL_SPAWNER_PURGE -ErrorAction SilentlyContinue
      # The program's own uninstall, when the program is there: it takes out
      # what it put on this PC as well, which only it knows of, then removes
      # itself, its folder and its PATH entry.
      $exe = Join-Path $binDir 'wsl-spawner.exe'
      if (Test-Path -LiteralPath $exe) {
        $uninstallArgs = @('uninstall', '--yes')
        if ($purgeAsked) { $uninstallArgs += '--purge' }
        & $exe @uninstallArgs
        $code = $LASTEXITCODE
        if ($code -ne 0) { throw "wsl-spawner uninstall: the program's own uninstall ended with $code; what it said is above" }
        return
      }
      # Without the program, an install cut short, or a program removed by
      # hand: its files, its sign-in program and its PATH entry go here.
      # Only the program can take its sign-out hook out of the local Group
      # Policy, as an administrator: while the hook runs this folder's build
      # with no console, nothing goes, and installing wsl-spawner again and
      # running its own uninstall takes all of it out. The policy is only
      # read here.
      # The quiet build as the program names it in the hook and the sign-in
      # program: its full path, with backslashes.
      $quiet = Join-Path ([IO.Path]::GetFullPath($binDir)) 'wsl-spawnerw.exe'
      $ini = Join-Path $env:SystemRoot 'System32\GroupPolicy\User\Scripts\scripts.ini'
      $hookText = ''
      try {
        if (Test-Path -LiteralPath $ini) {
          $b = [IO.File]::ReadAllBytes($ini)
          # UTF-16 LE with its mark, as the Group Policy editor writes it, or
          # one byte to a character: the program's name is found either way.
          $hookText = if ($b.Length -ge 2 -and $b[0] -eq 0xFF -and $b[1] -eq 0xFE) { [Text.Encoding]::Unicode.GetString($b, 2, $b.Length - 2) } else { [Text.Encoding]::UTF8.GetString($b) }
        }
      } catch {
        Write-Host "the sign-out scripts could not be read ($($_.Exception.Message)): if they still run $quiet, install wsl-spawner again and run wsl-spawner uninstall"
      }
      if ($hookText.IndexOf($quiet, [StringComparison]::OrdinalIgnoreCase) -ge 0) {
        throw "wsl-spawner uninstall: the sign-out hook in the local Group Policy runs $quiet; nothing was removed: install wsl-spawner again, then run wsl-spawner uninstall, which takes it out"
      }
      # A running wsl-spawner holds its files: removing them would fail half
      # way, so that is found out first.
      foreach ($f in $files) {
        $p = Join-Path $binDir $f
        if (Test-Path -LiteralPath $p) {
          try { [IO.File]::Open($p, 'Open', 'ReadWrite', 'None').Close() }
          catch { throw "wsl-spawner uninstall: $p is in use; close wsl-spawner and run this again" }
        }
      }
      # The sign-in program, when it starts this folder's program: it needs
      # no administrator, and left behind, every sign-in would start a
      # program that is gone. One that starts another copy stays. Task
      # Manager's note about it goes with it, as the program's own uninstall
      # takes it.
      $runKey = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Software\Microsoft\Windows\CurrentVersion\Run', $true)
      if ($runKey) {
        try {
          $run = [string]$runKey.GetValue('wsl-spawner', '')
          if ($run.IndexOf($quiet, [StringComparison]::OrdinalIgnoreCase) -ge 0) {
            $runKey.DeleteValue('wsl-spawner', $false)
            $approved = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\Run', $true)
            if ($approved) {
              try { $approved.DeleteValue('wsl-spawner', $false) } finally { $approved.Close() }
            }
            Write-Host 'took the sign-in program out'
          }
        } finally {
          $runKey.Close()
        }
      }
      # The programs, and the copies an install cut short left beside them.
      # What was there is counted, so a second uninstall does not claim to
      # have removed anything.
      $found = 0
      foreach ($f in $files) {
        foreach ($n in $f, "$f.new") {
          $p = Join-Path $binDir $n
          # Only a file: a folder of that name is not the program's, and
          # Remove-Item would take it, with what is in it.
          if (Test-Path -LiteralPath $p -PathType Leaf) {
            $found++
            Remove-Item -LiteralPath $p -Force -ErrorAction SilentlyContinue
          }
        }
      }
      if ($found) { Write-Host "removed wsl-spawner from $binDir" } else { Write-Host "wsl-spawner is not installed in $binDir" }
      # The folder goes too once nothing else is in it, and with it its PATH
      # entry, when it is named wsl-spawner, as the program's own uninstall
      # decides. A folder that holds other files, or one of the person's own
      # (WSL_SPAWNER_BIN_DIR set to it), which may hold the program alone,
      # keeps them and its entry: the install may not be what put it there.
      $own = (Split-Path -Leaf $binDir.TrimEnd('\', '/')) -eq 'wsl-spawner'
      if ((Test-Path -LiteralPath $binDir) -and (Get-ChildItem -LiteralPath $binDir -Force)) {
        Write-Host "kept $binDir and its PATH entry: other files are in it"
      } elseif (-not $own) {
        Write-Host "kept $binDir and any PATH entry for it: it is not named wsl-spawner"
      } else {
        Remove-Item -LiteralPath $binDir -Force -ErrorAction SilentlyContinue
        $key = & $pathKey
        try {
          $userPath = [string]$key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
          $newPath = & $withoutBinDir $userPath
          if ($null -ne $newPath) {
            $key.SetValue('Path', $newPath, (& $pathKind $key))
            & $announce
            Write-Host "took $binDir out of your PATH"
          }
        } finally {
          $key.Close()
        }
      }
      $data = Join-Path $env:APPDATA 'wsl-spawner'
      $state = Join-Path $env:LOCALAPPDATA 'wsl-spawner'
      if (-not $purgeAsked) {
        Write-Host "kept the settings in $data and the history in $state; WSL_SPAWNER_PURGE=1 removes them too"
      } else {
        # Only the files the program keeps there, by their names, as its own
        # uninstall --purge removes them; a folder goes once nothing else is
        # in it, and one with other files stays, with them.
        $purge = @(@{ Dir = $data; Names = '^config\.toml(\.new)?$' }, @{ Dir = $state; Names = '^(history\.jsonl|history-.*\.tmp|crash-[0-9]+-[0-9]+(-shown)?\.txt)$' })
        $left = @()
        foreach ($p in $purge) {
          if (-not (Test-Path -LiteralPath $p.Dir)) { continue }
          Get-ChildItem -LiteralPath $p.Dir -Force -File | Where-Object { $_.Name -cmatch $p.Names } | Remove-Item -Force -ErrorAction SilentlyContinue
          if (Get-ChildItem -LiteralPath $p.Dir -Force) { $left += $p.Dir } else { Remove-Item -LiteralPath $p.Dir -Force -ErrorAction SilentlyContinue }
        }
        if ($left.Count) { Write-Host "removed the settings and the history; kept $($left -join ' and '): other files are in it" } else { Write-Host 'removed the settings and the history' }
      }
      return
    }

    # x64 or ARM64 Windows. The machine's own architecture counts, not this
    # PowerShell's: an x64 or x86 PowerShell on ARM64 Windows runs emulated and
    # says AMD64 or X86, and the x64 launcher it would install cannot run in an
    # ARM64 distribution. The system's environment in the registry holds the
    # machine's, whatever the reader; .NET's OSArchitecture says it from .NET 5
    # (PowerShell 7) on, and a 32-bit PowerShell on x64 finds it in
    # PROCESSOR_ARCHITEW6432.
    $machine = $null
    try { $machine = [string](Get-ItemProperty -LiteralPath 'HKLM:\SYSTEM\CurrentControlSet\Control\Session Manager\Environment' -Name PROCESSOR_ARCHITECTURE -ErrorAction Stop).PROCESSOR_ARCHITECTURE } catch { }
    if (-not $machine) { try { $machine = [string][Runtime.InteropServices.RuntimeInformation]::OSArchitecture } catch { } }
    if (-not $machine) { $machine = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE } }
    $arch = switch ($machine) { { $_ -in 'AMD64', 'X64' } { 'amd64' } 'ARM64' { 'arm64' } default { throw "wsl-spawner install: no release for $machine" } }

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ('wsl-spawner-' + [Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
      if ($env:WSL_SPAWNER_ZIP) {
        $zip = $env:WSL_SPAWNER_ZIP
      } else {
        # The latest release is where github.com/<repo>/releases/latest redirects.
        $version = $env:WSL_SPAWNER_VERSION
        if (-not $version) {
          try {
            $req = [Net.WebRequest]::Create("https://github.com/$repo/releases/latest")
            $req.Method = 'HEAD'
            $resp = $req.GetResponse()
            $url = $resp.ResponseUri.AbsoluteUri
            $resp.Close()
          } catch {
            throw "wsl-spawner install: could not find the latest release: $($_.Exception.Message); check the connection, or set WSL_SPAWNER_VERSION, like v0.5.1"
          }
          if ($url -notmatch '/releases/tag/(.+)$') { throw 'wsl-spawner install: no release published yet; set WSL_SPAWNER_VERSION' }
          $version = $Matches[1]
        }
        if (-not $version.StartsWith('v')) { $version = "v$version" }
        $name = "wsl-spawner_$($version.Substring(1))_windows_$arch.zip"
        $zip = Join-Path $tmp $name
        Write-Host "downloading wsl-spawner $version for windows/$arch"
        $wc = New-Object Net.WebClient
        try {
          $wc.DownloadFile("$base/$version/$name", $zip)
          $wc.DownloadFile("$base/$version/checksums.txt", (Join-Path $tmp 'checksums.txt'))
        } catch {
          throw "wsl-spawner install: could not download $name from $base/${version}: $($_.Exception.Message); check WSL_SPAWNER_VERSION and the connection"
        }
        # The release's checksum. Get-FileHash is missing from some PowerShell
        # setups, so the hash is computed directly.
        $line = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { $_ -match (' ' + [regex]::Escape($name) + '$') } | Select-Object -First 1
        if (-not $line) { throw "wsl-spawner install: $name is not in checksums.txt" }
        $want = ($line -split ' ')[0]
        $sha = [Security.Cryptography.SHA256]::Create()
        $stream = [IO.File]::OpenRead($zip)
        try { $got = -join ($sha.ComputeHash($stream) | ForEach-Object { $_.ToString('x2') }) } finally { $stream.Close() }
        if ($got -ne $want) { throw "wsl-spawner install: checksum mismatch for ${name}: the download was damaged or changed; run the install again, and report it if it repeats" }
      }

      Expand-Archive -Path $zip -DestinationPath (Join-Path $tmp 'x') -Force
      New-Item -ItemType Directory -Path $binDir -Force | Out-Null
      # The Windows program, its build with no console, and the launcher WSL
      # finds by name on the PATH it takes from Windows ($files above).
      # A running wsl-spawner holds its files: find that out before anything
      # is replaced, so an install never leaves a new program beside an old
      # launcher.
      foreach ($f in $files) {
        $dest = Join-Path $binDir $f
        if (Test-Path -LiteralPath $dest) {
          try { [IO.File]::Open($dest, 'Open', 'ReadWrite', 'None').Close() }
          catch { throw "wsl-spawner install: $dest is in use; close wsl-spawner and run the install again" }
        }
      }
      # Each file lands beside its old copy first, then takes its place: a copy
      # cut short leaves the old one whole.
      foreach ($f in $files) {
        Copy-Item -LiteralPath (Join-Path $tmp "x\$f") -Destination (Join-Path $binDir "$f.new") -Force
      }
      foreach ($f in $files) {
        Move-Item -LiteralPath (Join-Path $binDir "$f.new") -Destination (Join-Path $binDir $f) -Force
      }
      Write-Host "installed $(Join-Path $binDir 'wsl-spawner.exe')"
    } finally {
      Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }

    # The user's PATH gets the folder once, for terminals opened from now on.
    $key = & $pathKey
    try {
      $userPath = [string]$key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
      $newPath = & $withBinDir $userPath
      if ($null -ne $newPath) {
        $key.SetValue('Path', $newPath, (& $pathKind $key))
        & $announce
        Write-Host "added $binDir to your PATH; open a new terminal, then run wsl-spawner"
      }
    } finally {
      $key.Close()
    }

    # In a console, a first install opens setup to choose the language, the
    # colors, the screen or the overview, the distributions and the sign-out
    # hook; an upgrade keeps the settings it has. WSL_SPAWNER_NO_SETUP=1
    # skips it, as does a script: a redirected console, or PowerShell
    # started with -NonInteractive, where nobody answers.
    $nonInteractive = [Environment]::GetCommandLineArgs() | Where-Object { $_ -match '^-noni' }
    $first = -not (Test-Path -LiteralPath (Join-Path $env:APPDATA 'wsl-spawner\config.toml'))
    if ($first -and $env:WSL_SPAWNER_NO_SETUP -ne '1' -and -not $nonInteractive -and -not [Console]::IsInputRedirected -and -not [Console]::IsOutputRedirected) {
      # The install is done whatever setup does: a setup that fails or is
      # left with Ctrl+C says so itself and opens again at the first start.
      try {
        & (Join-Path $binDir 'wsl-spawner.exe') setup
      } catch {
        Write-Host "wsl-spawner setup did not finish; wsl-spawner setup opens it again"
      }
    }
  } catch {
    # In a window, the reason alone, in red: PowerShell's own record of it
    # (the line, the category) gives a person nothing to act on. A script
    # gets the error itself, so its exit code says the install failed.
    if (-not $window) { throw }
    Write-Host $_.Exception.Message -ForegroundColor Red
  }
}
