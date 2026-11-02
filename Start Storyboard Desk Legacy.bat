@echo off
setlocal
pushd "%~dp0" || exit /b 1
where py >nul 2>nul
if errorlevel 1 (
    python -X utf8 launch.py --legacy %*
) else (
    py -3 -X utf8 launch.py --legacy %*
)
set "SBD_EXIT=%ERRORLEVEL%"
popd
if not "%SBD_EXIT%"=="0" pause
exit /b %SBD_EXIT%
