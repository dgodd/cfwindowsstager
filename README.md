# CF Windows Stager

## Build dockerfile

```powershell
docker build -t dgodd/windows2016fs -f images\Dockerfile.windows .\images\
```

## Example run

```powershell
go build

.\cfwindowsstager.exe --image fixme/myapp --app ..\hwc-buildpack\fixtures\windows_app\ --buildpack ..\hwc-buildpack\hwc_buildpack-cached-windows2016-v3.1.3.zip

docker run -e PORT=8080 -p 8080:8080 fixme/myapp
```