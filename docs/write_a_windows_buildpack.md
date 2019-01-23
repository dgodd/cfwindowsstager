# Writing a windows buildpack

(**Note:** if you need to write an extension buildpack, please read [windows extension buildpack](https://github.com/dgodd/cfwindowsstager/blob/docs/docs/extension_buildpack.md) instead)

To demonstrare writing a windows buildpack, we are going to write a buildpack
for [go](https://golang.org/). You can wee the completed version of this
buildpack at
[go-windows-buildpack](https://github.com/dgodd/go-windows-buildpack).

## Supplying `go`

Windows buildpacks need to provide `bin/supply.bat` or `bin/supply.exe`,
however for our convenience we are going to write this buildpack in powershell.
This means that we need to provide `bin/supply.bat` to pass control to
`bin/supply.ps1`:

**bin/supply.bat**
```bat
@ECHO OFF
SET ScriptDir=%~dp0
PowerShell -NoProfile -ExecutionPolicy Bypass -Command "& '%ScriptDir%supply.ps1' %1 %2 %3 %4";
```

Looking the the above we can guess that 4 arguments are being passed through, in powershell we can name them, and so the start of our supply script should be:

```powershell
[CmdletBinding()]
Param(
[Parameter(Mandatory=$True,Position=1)] [string]$BuildDir,
[Parameter(Mandatory=$True,Position=2)] [string]$CacheDir,
[Parameter(Mandatory=$True,Position=3)] [string]$DepsDir,
[Parameter(Mandatory=$True,Position=4)] [string]$DepsIdx
)
$ErrorActionPreference = "Stop"
```

We will use these values shortly, but in essence:
* `BuildDir` is the directory that the users application was placed in
* `CacheDir` is a directory which can store things between subsequent staging
  runs. This can be useful to store downloaded files, or randomly generated
  files which we would like to be random per application, but stay the same
  each time we stage (eg. keys for reading/writing cookies).
* `DepsDir` is the root directory for all buildpacks to write to
* `DepsIdx` the index for **this** buildpack. We should write to `$DepsDir/$DepsIdx`.

We have also set `$ErrorActionPreference` to `Stop`, this is important so that
if a portion of the script fails, staging will fail.

At this point we can download and extract the windows binaries

```powershell
(New-Object System.Net.WebClient).DownloadFile('https://dl.google.com/go/go1.11.4.windows-amd64.zip', "$CacheDir/go.zip")
Expand-Archive -Path "$CacheDir/go.zip" -DestinationPath "$DepsDir/$DepsIdx" -Force
```

Unfortunately the zip file that I download has all the files which we want
nested inside a directory called `go`, this means that having extracted the zip
file, the executables are now at `$DepsDir/$DepsIdx/**go**/bin/` instead of
`$DepsDir/$DepsIdx/bin/`. We can solve this (and make sure that our binaries
are on the path) by moving everything up one directory:

```powershell
Move-Item -Path "$DepsDir/$DepsIdx/go/*" -Destination "$DepsDir/$DepsIdx"
Remove-Item -Path "$DepsDir/$DepsIdx/go"
```

At this stage our buildpack can work as an extension buildpack to provide a
`go` compiler to other buildpacks, in the next step we see how to finish our
buildpack so that it can be used as a full buildpack and compile the
application.

## Building the users application

Now that we have go, we can compile the users application (`go build`). We choose to do that inside `bin/finalize.ps1` so that this buildpack can be used as either an extension buildpack or a full buildpack:

**bin/finalize.bat**
```bat
@ECHO OFF
SET ScriptDir=%~dp0
PowerShell -NoProfile -ExecutionPolicy Bypass -Command "& '%ScriptDir%finalize.ps1' %1 %2 %3 %4";
```

**bin/finalize.ps1**
```powershell
[CmdletBinding()]
Param(
[Parameter(Mandatory=$True,Position=1)] [string]$BuildDir,
[Parameter(Mandatory=$True,Position=2)] [string]$CacheDir,
[Parameter(Mandatory=$True,Position=3)] [string]$DepsDir,
[Parameter(Mandatory=$True,Position=4)] [string]$DepsIdx
)

$ErrorActionPreference = "Stop"
$env:PATH += ";$DepsDir/$DepsIdx/bin"

Write-Output "-----> Build app"
Set-Location $BuildDir
go build -o myapp.exe .
if ($lastexitcode -ne 0) {
    Write-Output "ERROR building app with go"
    Write-Output "EXITCODE: $lastexitcode"
    Exit 1
}
```

We see the same setting of `$BuildDir` et.al. and then we add to the path,
whilst `$DepsDir/*/bin` will be added to the path at runtime, we need to add go
to our own path.

The next thing to notice is that we need to test that `go build` succeeded
using `$lastexitcode` and exit with a non-zero if it failed.

## Final setup

At this point we have written most of the code for a buildpack. However, to
make sure that we can use `extension` buildpacks, we need to set those up at
the end of `finalize.ps1` and so we add

```powershell
Write-Output "-----> Configure runtime path and env vars"
$dirs = Get-Childitem -Path $DepsDir | where{ Join-Path $_.FullName "bin" | Test-Path } | %{ '%DEPS_DIR%\' + $_.Name + '\bin' } | &{$ofs=';';"$input"}
Set-Content -Path "$DepsDir/../profile.d/000_multi-supply.bat" -Value "set PATH=$dirs;%PATH%"
Foreach ($d in (Get-Childitem -Path $DepsDir | where{ Join-Path $_.FullName "profile.d" | Test-Path })) {
    Foreach ($f in (Get-Childitem -Path "$DepsDir/$d/profile.d")) {
        Copy-Item $f.FullName -Destination "$DepsDir/../profile.d/${d}_${f}"
    }
}
```

The astute will notice that while I said that files in `$DepsDir/*/bin` would
be on the path, the above code is what actually does it. Buildpacks have an
implicit contract that they will set those paths up during `finalize`, however,
so that buildpacks could choose to _not_ do this the contract was made
`implicit` rather than automatic.

## Conclusion

We have now written a buildpack for windows. You can see the completed version
of this buildpack at
[go-windows-buildpack](https://github.com/dgodd/go-windows-buildpack).
