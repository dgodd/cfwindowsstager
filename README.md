# CF Windows Stager

CF Windows Stager is designed to be a quick way to run Cloud Foundry buildpacks
on an application.

This is particularly helpful when building a buildpack (or extension buildpack)
where quick run loops are very helpful. Normally when testing buildpacks you
need to package them up, create them in CF (which requires admin permissions)
and then stage an app. Using CF Windows Stager, you can package the app, and
instantly stage an app against it, all locally on your windows machine.

To download and run executables see [releases](https://github.com/dgodd/cfwindowsstager/releases)

For descriptions of creating buildpacks, [docs](https://github.com/dgodd/cfwindowsstager/tree/master/docs)

This tool was inspired by
[cflocal](https://github.com/cloudfoundry-incubator/cflocal) and
[pack](https://github.com/buildpack/pack) (both of which are currently linux
only, whilst this tool runs best on windows).

## Example run

```powershell
go build

.\cfwindowsstager.exe --image fixme/myapp --app ..\hwc-buildpack\fixtures\windows_app\ --buildpack ..\hwc-buildpack\hwc_buildpack-cached-windows2016-v3.1.3.zip

docker run --rm -e PORT=8080 -p 8080:8080 cfwindowsstager/myapp
```

Then to see the results `Invoke-WebRequest -Uri http://127.0.0.1:8080/ -UseBasicParsing`

## Build lifecycle.tar.gz

```bash
go run ./images/
```

## Build executables

The executables on the release page 

```bash
GOOS=linux go build -o cfwindowsstager.linux .
GOOS=darwin go build -o cfwindowsstager.darwin .
GOOS=windows go build -o cfwindowsstager.exe .
upx cfwindowsstager.{darwin,linux,exe}
```
