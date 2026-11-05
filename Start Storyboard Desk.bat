@echo off
setlocal
pushd "%~dp0" || exit /b 1
if defined STORYBOARD_GO_EXECUTABLE (
    "%STORYBOARD_GO_EXECUTABLE%" %*
) else if exist "build\gosx\storyboard-desk-go.exe" (
    "build\gosx\storyboard-desk-go.exe" %*
) else if exist "gosx-host\storyboard-desk-go.exe" (
    "gosx-host\storyboard-desk-go.exe" %*
) else (
    go -C "gosx-host" run ./cmd/client 1>&2
    if errorlevel 1 exit /b 1
    go -C "gosx-host" run . %*
)
set "SBD_EXIT=%ERRORLEVEL%"
popd
if not "%SBD_EXIT%"=="0" (
    echo GoSX could not start. See docs\GOSX_PORT.md. Use -browser for browser mode.
    pause
)
exit /b %SBD_EXIT%
