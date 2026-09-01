#!/bin/bash
set -e

# Asegurar permisos de root
if [ "$EUID" -ne 0 ]; then
  echo "Por favor ejecuta este script como root (sudo ./uninstall.sh)"
  exit 1
fi

echo "=========================================="
echo " Desinstalador de BackiieTUI para Linux"
echo "=========================================="

echo "[1/4] Deteniendo y deshabilitando el servicio..."
if systemctl is-active --quiet backiie.service; then
    sudo systemctl stop backiie.service
fi
if systemctl is-enabled --quiet backiie.service 2>/dev/null; then
    sudo systemctl disable backiie.service
fi

echo "[2/4] Eliminando archivos del sistema (Systemd)..."
rm -f /etc/systemd/system/backiie.service
sudo systemctl daemon-reload

echo "[3/4] Eliminando binarios..."
rm -f /usr/local/bin/backiietui_bin
rm -f /usr/local/bin/backiie

echo "[4/4] Limpiando base de datos y logs locales..."
rm -rf /var/lib/backiie
rm -f /var/log/backiie.log

echo "=========================================="
echo " Desinstalacion completada. El sistema esta limpio."
echo "=========================================="
