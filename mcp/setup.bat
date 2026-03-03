@echo off
echo Installing MCP Search Server dependencies...
python -m pip install -r requirements.txt
echo.
echo Installation complete!
echo.
echo To test the server, run: python test_search.py
pause
