@echo off
pwsh -NoProfile -ExecutionPolicy Bypass -File "%~dp0Start-Developer-Assistant.ps1" %*
if errorlevel 1 pause
