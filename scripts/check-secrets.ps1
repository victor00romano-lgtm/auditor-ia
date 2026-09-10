param([string]$Allowlist = ".secretsignore")
$patterns = @(
  @{ Type = "Bitrix webhook"; Regex = 'https?://[^\s]+/rest/\d+/[A-Za-z0-9_-]{8,}' },
  @{ Type = "Database password"; Regex = 'postgres(?:ql)?://[^:\s/]+:[^@\s/]+@' },
  @{ Type = "Bearer token"; Regex = '(?i)Bearer\s+[A-Za-z0-9._~+/=-]{12,}' },
  @{ Type = "JWT"; Regex = 'eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+' }
)
$ignored = @(); if (Test-Path -LiteralPath $Allowlist) { $ignored = Get-Content -LiteralPath $Allowlist | Where-Object { $_ -and -not $_.StartsWith('#') } }
$files = git ls-files 2>$null; if ($LASTEXITCODE -ne 0) { $files = rg --files }
$found = $false
foreach ($file in $files) {
  $normalizedFile = $file.Replace('\', '/')
  if ($ignored -contains $normalizedFile -or -not (Test-Path -LiteralPath $file)) { continue }
  $lineNumber = 0
  foreach ($line in Get-Content -LiteralPath $file -ErrorAction SilentlyContinue) {
    $lineNumber++
    foreach ($pattern in $patterns) {
      if ($line -match $pattern.Regex) { Write-Error "$file`:$lineNumber possível $($pattern.Type) [valor mascarado]"; $found = $true }
    }
  }
}
if ($found) { exit 1 }; Write-Output "Nenhum segredo provável encontrado nos arquivos examinados."
