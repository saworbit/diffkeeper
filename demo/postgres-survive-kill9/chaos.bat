@echo off
setlocal enabledelayedexpansion

echo Chaos monkey started (Windows) - will randomly send SIGKILL to Postgres.
echo Watch the pgbench count in another terminal - it should never reset.
echo Press Ctrl+C to stop.

:loop
set target=
for /f "usebackq tokens=*" %%i in (`docker compose ps -q postgres 2^>nul`) do set target=%%i
if "%target%"=="" (
  echo %date% %time% waiting for container...
  timeout /t 3 >nul
  goto loop
)
set /a delay=%RANDOM% %% 8 + 3
timeout /t %delay% >nul
echo %date% %time% sending kill -9 to %target%
docker kill -s KILL %target% >nul 2>&1
goto loop
