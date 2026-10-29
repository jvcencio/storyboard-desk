@echo off
cd /d "%~dp0"
where py >nul 2>nul
if errorlevel 1 (
    python -X utf8 launch.py
) else (
    py -3 -X utf8 launch.py
)
if errorlevel 1 pause
