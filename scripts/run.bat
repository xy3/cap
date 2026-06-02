@echo off
rem Launch the Capper styling UI.
rem ffmpeg.exe / ffprobe.exe / whisper-cli.exe sit next to capper.exe; capper
rem prepends its own folder to PATH at startup so it finds them with no setup.
rem
rem CAPPER_RELAUNCH tells capper it can auto-restart after an in-app update:
rem when you click "Update" in the UI, capper installs the new version and exits
rem with code 42, and this loop relaunches the updated binary on the same port.
cd /d "%~dp0"
set CAPPER_RELAUNCH=1

echo Starting Capper UI on http://localhost:8080 ...
echo (Close this window to stop the server.)
start "" http://localhost:8080

:loop
capper.exe serve
if %errorlevel%==42 (
    echo Update installed - restarting Capper...
    goto loop
)

pause
