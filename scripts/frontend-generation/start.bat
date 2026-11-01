@echo off
setlocal
cd /d "%~dp0"
py -3 launch.py %*
if errorlevel 1 (
  echo.
  echo Python Launcher [py] failed; trying python directly.
  python launch.py %*
)
pause
