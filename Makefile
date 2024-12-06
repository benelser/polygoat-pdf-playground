PDF:=powershell.pdf
REPO:=https://github.com/PowerShell/PowerShell.git
OUT:=./exfil

poc:
	@go run main.go embed --repo $(REPO) --output $(PDF)
	@echo "Simulating data exfiltration... PDF created with embedded repository."
	@go run main.go extract --input $(PDF) --output $(OUT)
	@echo "Data successfully exfiltrated! Repository cloned to $(OUT)."
