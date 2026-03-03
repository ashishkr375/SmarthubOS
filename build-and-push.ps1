# Build and Push Docker Images to Docker Hub
# Usage: .\build-and-push.ps1 <dockerhub-username> [version]

param(
    [Parameter(Mandatory=$true)]
    [string]$DockerUsername,
    
    [Parameter(Mandatory=$false)]
    [string]$Version = "latest"
)

$ErrorActionPreference = "Stop"

Write-Host "==========================================" -ForegroundColor Cyan
Write-Host "Docker Build and Push Script" -ForegroundColor Cyan
Write-Host "==========================================" -ForegroundColor Cyan
Write-Host ""

if ($Version -eq "latest") {
    Write-Host "No version specified, using 'latest'" -ForegroundColor Yellow
} else {
    Write-Host "Using version: $Version" -ForegroundColor Green
}

Write-Host "Docker Hub Username: $DockerUsername" -ForegroundColor Green
Write-Host ""

# Check if Docker is running
Write-Host "Checking Docker..." -ForegroundColor Yellow
try {
    docker info | Out-Null
    Write-Host "[OK] Docker is running" -ForegroundColor Green
} catch {
    Write-Host "Error: Docker is not running!" -ForegroundColor Red
    Write-Host "Please start Docker Desktop and try again." -ForegroundColor Red
    exit 1
}
Write-Host ""

# Login to Docker Hub
Write-Host "Logging in to Docker Hub..." -ForegroundColor Yellow
try {
    docker login
    if ($LASTEXITCODE -ne 0) { throw "Login failed" }
    Write-Host "[OK] Logged in to Docker Hub" -ForegroundColor Green
} catch {
    Write-Host "Error: Docker login failed!" -ForegroundColor Red
    exit 1
}
Write-Host ""

# Build images
Write-Host "==========================================" -ForegroundColor Cyan
Write-Host "Building Docker Images..." -ForegroundColor Cyan
Write-Host "==========================================" -ForegroundColor Cyan
Write-Host ""

Write-Host "[1/3] Building Web image..." -ForegroundColor Yellow
docker build --target web `
    -t "${DockerUsername}/erp-web:${Version}" `
    -t "${DockerUsername}/erp-web:latest" `
    --build-arg NEXT_PUBLIC_API_URL=https://erpapi.eduteria.info `
    .
if ($LASTEXITCODE -ne 0) {
    Write-Host "Error: Web image build failed!" -ForegroundColor Red
    exit 1
}
Write-Host "[OK] Web image built successfully" -ForegroundColor Green
Write-Host ""

Write-Host "[2/3] Building API image..." -ForegroundColor Yellow
docker build --target api `
    -t "${DockerUsername}/erp-api:${Version}" `
    -t "${DockerUsername}/erp-api:latest" `
    .
if ($LASTEXITCODE -ne 0) {
    Write-Host "Error: API image build failed!" -ForegroundColor Red
    exit 1
}
Write-Host "[OK] API image built successfully" -ForegroundColor Green
Write-Host ""

Write-Host "[3/3] Building Worker image..." -ForegroundColor Yellow
docker build --target worker `
    -t "${DockerUsername}/erp-worker:${Version}" `
    -t "${DockerUsername}/erp-worker:latest" `
    .
if ($LASTEXITCODE -ne 0) {
    Write-Host "Error: Worker image build failed!" -ForegroundColor Red
    exit 1
}
Write-Host "[OK] Worker image built successfully" -ForegroundColor Green
Write-Host ""

# Push images
Write-Host "==========================================" -ForegroundColor Cyan
Write-Host "Pushing Images to Docker Hub..." -ForegroundColor Cyan
Write-Host "==========================================" -ForegroundColor Cyan
Write-Host ""

Write-Host "[1/6] Pushing Web:${Version}..." -ForegroundColor Yellow
docker push "${DockerUsername}/erp-web:${Version}"
if ($LASTEXITCODE -ne 0) {
    Write-Host "Error: Failed to push Web:${Version}" -ForegroundColor Red
    exit 1
}

Write-Host "[2/6] Pushing Web:latest..." -ForegroundColor Yellow
docker push "${DockerUsername}/erp-web:latest"
if ($LASTEXITCODE -ne 0) {
    Write-Host "Error: Failed to push Web:latest" -ForegroundColor Red
    exit 1
}

Write-Host "[3/6] Pushing API:${Version}..." -ForegroundColor Yellow
docker push "${DockerUsername}/erp-api:${Version}"
if ($LASTEXITCODE -ne 0) {
    Write-Host "Error: Failed to push API:${Version}" -ForegroundColor Red
    exit 1
}

Write-Host "[4/6] Pushing API:latest..." -ForegroundColor Yellow
docker push "${DockerUsername}/erp-api:latest"
if ($LASTEXITCODE -ne 0) {
    Write-Host "Error: Failed to push API:latest" -ForegroundColor Red
    exit 1
}

Write-Host "[5/6] Pushing Worker:${Version}..." -ForegroundColor Yellow
docker push "${DockerUsername}/erp-worker:${Version}"
if ($LASTEXITCODE -ne 0) {
    Write-Host "Error: Failed to push Worker:${Version}" -ForegroundColor Red
    exit 1
}

Write-Host "[6/6] Pushing Worker:latest..." -ForegroundColor Yellow
docker push "${DockerUsername}/erp-worker:latest"
if ($LASTEXITCODE -ne 0) {
    Write-Host "Error: Failed to push Worker:latest" -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "==========================================" -ForegroundColor Green
Write-Host "[SUCCESS] All images built and pushed!" -ForegroundColor Green
Write-Host "==========================================" -ForegroundColor Green
Write-Host ""
Write-Host "Images pushed:" -ForegroundColor Cyan
Write-Host "  - ${DockerUsername}/erp-web:${Version}"
Write-Host "  - ${DockerUsername}/erp-web:latest"
Write-Host "  - ${DockerUsername}/erp-api:${Version}"
Write-Host "  - ${DockerUsername}/erp-api:latest"
Write-Host "  - ${DockerUsername}/erp-worker:${Version}"
Write-Host "  - ${DockerUsername}/erp-worker:latest"
Write-Host ""
Write-Host "Next steps:" -ForegroundColor Yellow
Write-Host "1. SSH into your VPS"
Write-Host "2. Run: docker pull ${DockerUsername}/erp-web:latest"
Write-Host "3. Run: docker pull ${DockerUsername}/erp-api:latest"
Write-Host "4. Run: docker pull ${DockerUsername}/erp-worker:latest"
Write-Host "5. Deploy containers (see DEPLOYMENT.md)"
Write-Host ""
