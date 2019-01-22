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
`supply.ps1`. To start with we will 
