@echo off
setlocal
cd /d "%~dp0"
py -3 --version >nul 2>nul
if errorlevel 1 (
  echo Python Launcher not available; trying python directly.
  python launch.py %*
) else (
  py -3 launch.py %*
)
pause
