#!/bin/bash

set -e

# Alpine Linux version
ALPINE_VERSION="3.19"
ALPINE_MINOR="3.19.1"
ARCH="x86_64"

# rootfs directory
ROOTFS_DIR="/var/lib/container-runtime/rootfs/alpine"

echo "=========================================="
echo "Alpine Linux rootfs Installation Script"
echo "=========================================="
echo "Version: Alpine Linux ${ALPINE_MINOR}"
echo "Target Directory: ${ROOTFS_DIR}"
echo "=========================================="

echo "[1/5] Creating rootfs directory..."
sudo mkdir -p "${ROOTFS_DIR}"

echo "[2/5] Downloading Alpine minirootfs..."
DOWNLOAD_URL="https://dl-cdn.alpinelinux.org/alpine/v${ALPINE_VERSION}/releases/${ARCH}/alpine-minirootfs-${ALPINE_MINOR}-${ARCH}.tar.gz"
TARBALL="/tmp/alpine-minirootfs-${ALPINE_MINOR}-${ARCH}.tar.gz"

if [ -f "${TARBALL}" ]; then
    echo "Tarball already exists: ${TARBALL}"
    read -p "Download again? (y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        sudo rm "${TARBALL}"
        sudo wget -O "${TARBALL}" "${DOWNLOAD_URL}"
    fi
else
    sudo wget -O "${TARBALL}" "${DOWNLOAD_URL}"
fi

echo "[3/5] Extracting rootfs..."
sudo tar -xzf "${TARBALL}" -C "${ROOTFS_DIR}"

echo "[4/5] Creating required directories..."
for dir in proc sys dev tmp run; do
    sudo mkdir -p "${ROOTFS_DIR}/${dir}"
done

echo "[5/5] Setting permissions..."
sudo chmod 755 "${ROOTFS_DIR}"
sudo chmod 1777 "${ROOTFS_DIR}/tmp"

echo "=========================================="
echo "Installation Complete!"
echo "=========================================="
echo "rootfs path: ${ROOTFS_DIR}"
echo ""
echo "Installed contents:"
sudo ls -la "${ROOTFS_DIR}"
echo ""
echo "=========================================="
echo "Usage:"
echo "=========================================="
echo ""
echo "1. Run in interactive mode:"
echo "   sudo ./container-runtime run --rootfs ${ROOTFS_DIR} --command /bin/sh"
echo ""
echo "2. Run in detach mode:"
echo "   sudo ./container-runtime run -d --rootfs ${ROOTFS_DIR} --command /bin/sleep 3600"
echo ""
echo "3. Verify Alpine inside container:"
echo "   cat /etc/os-release"
echo "   apk --version"
echo ""
echo "=========================================="

read -p "Delete downloaded tarball? (y/N): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    sudo rm "${TARBALL}"
    echo "Tarball deleted: ${TARBALL}"
fi

echo ""
echo "Installation script finished"
