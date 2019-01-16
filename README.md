# CF Windows Stager

## Build executables

```bash
GOOS=linux go build -o cfwindowsstager.linux .
GOOS=darwin go build -o cfwindowsstager.darwin .
GOOS=windows go build -o cfwindowsstager.exe .
upx cfwindowsstager.{darwin,linux,exe}
```

## Build lifecycle.tar.gz

```bash
go run ./images/
```

## Example run

```powershell
go build

.\cfwindowsstager.exe --image fixme/myapp --app ..\hwc-buildpack\fixtures\windows_app\ --buildpack ..\hwc-buildpack\hwc_buildpack-cached-windows2016-v3.1.3.zip

docker run --rm -e PORT=8080 -p 8080:8080 cfwindowsstager/myapp
```

Then to see the results `Invoke-WebRequest -Uri http://127.0.0.1:8080/ -UseBasicParsing`