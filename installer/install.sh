#!/bin/bash
set -e

# Asegurar permisos de root
if [ "$EUID" -ne 0 ]; then
  echo "Por favor ejecuta este script como root (sudo ./install.sh)"
  exit 1
fi

echo "=========================================="
echo " Instalador de BackiieTUI para Linux"
echo "=========================================="

echo "[1/5] Compilando el binario..."
(cd .. && go build -o installer/backiietui ./cmd/main.go ./cmd/recover.go)

echo "[2/5] Moviendo binarios a /usr/local/bin..."
mv backiietui /usr/local/bin/backiietui_bin
chmod +x /usr/local/bin/backiietui_bin

echo "[3/5] Creando wrapper inteligente 'backiie'..."
# Como la base de datos bbolt solo permite un proceso a la vez, 
# el wrapper detiene el servicio en segundo plano mientras usas la interfaz,
# y lo vuelve a arrancar automaticamente cuando la cierras.
cat << 'EOF' > /usr/local/bin/backiie
#!/bin/bash
echo "Deteniendo servicio en segundo plano (para liberar la base de datos)..."
sudo systemctl stop backiie.service

export BACKIIE_DB_PATH="/var/lib/backiie/backiie.db"
export BACKIIE_LOG_FILE="/var/log/backiie.log"

/usr/local/bin/backiietui_bin "$@"

echo "Reiniciando servicio en segundo plano..."
sudo systemctl start backiie.service
EOF
chmod +x /usr/local/bin/backiie

echo "[4/5] Creando directorio para la base de datos..."
mkdir -p /var/lib/backiie
chmod 777 /var/lib/backiie

echo "[5/5] Configurando el servicio de Systemd (Demonio de respaldos)..."
cat << 'EOF' > /etc/systemd/system/backiie.service
[Unit]
Description=Backiie Scheduler Daemon
After=network.target

[Service]
Type=simple
# Se ejecuta sin TTY, por lo que BackiieTUI activara su modo headless automaticamente
ExecStart=/usr/local/bin/backiietui_bin
Restart=on-failure
RestartSec=5

# Variables de entorno compartidas
Environment="BACKIIE_DB_PATH=/var/lib/backiie/backiie.db"
Environment="BACKIIE_LOG_FILE=/var/log/backiie.log"

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable backiie.service
systemctl start backiie.service

echo "=========================================="
echo " Instalacion completada con exito!"
echo "=========================================="
echo " El servicio ya esta corriendo en segundo plano: systemctl status backiie"
echo " Los logs se guardan en: /var/log/backiie.log"
echo " La base de datos vive en: /var/lib/backiie/backiie.db"
echo ""
echo "Para abrir la interfaz, simplemente escribe en cualquier terminal:"
echo "  sudo backiie"
echo "=========================================="
