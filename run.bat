@echo off
title TxShD AutoCut Workstation
echo ==================================================
echo      TxShD Video Pipeline (AutoCut Workstation)
echo ==================================================
echo Menjalankan engine AutoCut di http://localhost:8080...
start http://localhost:8080
autocut-server.exe
pause
