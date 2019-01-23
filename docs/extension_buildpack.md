# Writing an extension buildpack

The simplest answer to writing an extension buildpack is that you are required
to provide a `bin/supply` executable. On Windows that means that you need to
provide either `bin/supply.bat` or `bin/supply.exe` and these will be run
during staging.

In this example we are going to use powershell for our supply script, this
means that we need to use a batch file to pass control to powershell

**bin/supply.bat**
```bat
@ECHO OFF
SET ScriptDir=%~dp0
PowerShell -NoProfile -ExecutionPolicy Bypass -Command "& '%ScriptDir%supply.ps1' %1 %2 %3 %4";
```

We can now start working on the interesting part of our extension buildpack,
`supply.ps1`.

## Provide runtime environment variables

Our first task is to set an environment variable at run time. To set an
environment variable we need `bin/supply.ps1` to create a bat file in the
profile.d directory:

**bin/supply.ps1**
```powershell
New-Item -ItemType directory -Path "$DepsDir/$DepsIdx/profile.d" | Out-Null
Set-Content -Path "$DepsDir/$DepsIdx/profile.d/mysupplied.bat" -Value 'set MyTestVariable="My temporary test variable."'
```

You will note in the above that reference several variables (`$DepsDir` and
`$DepsIdx`), these are variables provided as arguments to supply scripts, and
we can read them in supply.ps1 using:

```powershell
[CmdletBinding()]
Param(
[Parameter(Mandatory=$True,Position=1)] [string]$BuildDir,
[Parameter(Mandatory=$True,Position=2)] [string]$CacheDir,
[Parameter(Mandatory=$True,Position=3)] [string]$DepsDir,
[Parameter(Mandatory=$True,Position=4)] [string]$DepsIdx
)
$ErrorActionPreference = "Stop"

New-Item -ItemType directory -Path "$DepsDir/$DepsIdx/profile.d" | Out-Null
Set-Content -Path "$DepsDir/$DepsIdx/profile.d/mysupplied.bat" -Value 'set MyTestVariable="My temporary test variable."'
```

We are almsot ready to zip these files up and create a distributable
`buildpack`, there is one slight catch, to upload a `buildpack` to cloudfoundry
we need to provide `bin/compile`. I recommend providing an empty file for
`bin/compile`. So having created all three files above (`bin/supply.bat`,
`bin/supply.ps1` and `bin/compile`) we can zip them up:

```powershell
Compress-Archive -LiteralPath bin -CompressionLevel Optimal -DestinationPath example_buildpack.zip -Force
```

Having created a buildpack, we could use it to create a buildpack on
cloudfoundry ('cf create-buildpack') or we could use
[cfwindowsstager](https://github.com/dgodd/cfwindowsstager) to run it locally.
To test this out we are going to to use the [hwc-buildpack](https://github.com/cloudfoundry/hwc-buildpack) and a sample
application from that same buildpack:

```powershell
cfwindowsstager.exe --app ..\hwc-buildpack\fixtures\windows_app\ --buildpack example_buildpack.zip --buildpack https://github.com/cloudfoundry/hwc-buildpack/releases/download/v3.1.4/hwc-buildpack-windows2016-v3.1.4.zip
```

After running the created docker image (with the command that cfwindowsstager
provides in it's outut) we should be able to request the environment variables
for the running webapp over http

```powershell
Invoke-WebRequest -Uri http://127.0.0.1:8080/env -UseBasicParsing
```

and part of the output should be `MyTestVariable="My temporary test variable."`.

## Provide files and have them on the path

Any files which the buildpack places in `$DepsDir/$DepsIdx/bin` will be placed
on the path for the running container. Often we would do this by downloading
and extracting a zip file from the internet by using a combination of
`(New-Object System.Net.WebClient).DownloadFile` and `Expand-Archive`. However
today we are going to simply write a file to disk using:

```powershell
New-Item -ItemType directory -Path "$DepsDir/$DepsIdx/bin" | Out-Null
Set-Content -Path "$DepsDir/$DepsIdx/bin/mysupplied.bat" -Value 'echo "Hi from file on path, mysupplied.bat"'
```

This means that if our application executed `mysupplied.bat` during run time,
then windows would find our supplied file on the path and run it (much more
convenient than needing to know the full path to the file).

We can see an example the above described buildpack at
[example-extension-buildpack](https://github.com/dgodd/example-extension-buildpack)
and a sample program which takes advantage of a sample
[go-windows-buildpack](https://github.com/dgodd/go-windows-buildpack), so
having download
[example-extension-buildpack](https://github.com/dgodd/example-extension-buildpack):

```powershell
.\scripts\build.ps1
cfwindowsstager.exe --app .\fixtures\simple\ --buildpack .\example_extension_buildpack-windows2016-v0.1.2.zip --buildpack https://github.com/dgodd/go-windows-buildpack/releases/download/v0.0.2/go_buildpack-windows2016-v0.0.2.zip
```

Now (having run this new image) we can see that
```powershell
Invoke-WebRequest -Uri http://127.0.0.1:8080/-UseBasicParsing
```
calls `mysupllied.bat` and displays the text we provided.

## Next steps

This above should provide everything required to write an extension buildpack, however, if instead you need to write a full buildpack, see [write a buildpack](https://github.com/dgodd/cfwindowsstager/blob/docs/docs/write_a_windows_buildpack.md)
