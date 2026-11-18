param(
    [Parameter(Mandatory = $true)][string]$OutputFile,
    [switch]$ValidateOnly
)
$ErrorActionPreference = 'Stop'
try {
    $outputPath = [IO.Path]::GetFullPath($OutputFile)
    if (!$ValidateOnly) {
        New-Item -ItemType Directory -Path (Split-Path $outputPath -Parent) -Force | Out-Null
        # Official Evergreen Bootstrapper link from Microsoft's WebView2 download page.
        Invoke-WebRequest -UseBasicParsing -Uri 'https://go.microsoft.com/fwlink/p/?LinkId=2124703' -OutFile $outputPath
    }
    $signature = Get-AuthenticodeSignature -LiteralPath $outputPath
    if ($signature.Status -ne 'Valid' -or !$signature.SignerCertificate -or
        $signature.SignerCertificate.GetNameInfo([Security.Cryptography.X509Certificates.X509NameType]::SimpleName, $false) -cne 'Microsoft Corporation') {
        throw "WebView2 bootstrapper must have a valid Microsoft Corporation Authenticode signature: $($signature.Status)"
    }
    Write-Host "Verified Microsoft WebView2 bootstrapper: $outputPath"
} catch {
    Write-Error $_
    exit 1
}
