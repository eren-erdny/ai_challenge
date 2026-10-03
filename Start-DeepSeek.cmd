@echo off
chcp 65001 >nul
cd /d "%~dp0"
set "GOCACHE=%~dp0.gocache"
go run ./projects/deepseek-client
if errorlevel 1 pause
