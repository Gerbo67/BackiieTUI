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

echo "[1/7] Verificando dependencias de bases de datos (PostgreSQL Client)..."
if ! command -v pg_dump &> /dev/null; then
    echo "=========================================="
    echo " No se detectó 'pg_dump' en el sistema."
    echo " BackiieTUI lo requiere para respaldar PostgreSQL."
    echo " ¿Qué versión de PostgreSQL Client deseas instalar?"
    echo " (Debe ser IGUAL o MAYOR a la versión de tu servidor)"
    echo " 1) Versión 17 (Estándar de Linux / Rápida)"
    echo " 2) Versión 18 (Desde el repositorio oficial de Postgres)"
    echo " 3) Saltar (Lo instalaré manualmente o usaré Docker)"
    read -p "Selecciona una opción [1-3]: " pg_opt

    if [ "$pg_opt" == "1" ]; then
        echo "Instalando postgresql-client (v17)..."
        apt-get update && apt-get install -y postgresql-client
    elif [ "$pg_opt" == "2" ]; then
        echo "Instalando postgresql-client-18 desde repositorio oficial..."
        apt-get update
        apt-get install -y curl ca-certificates lsb-release
        install -d /usr/share/postgresql-common/pgdg
        curl -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc --fail https://www.postgresql.org/media/keys/ACCC4CF8.asc
        sh -c 'echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc] https://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" > /etc/apt/sources.list.d/pgdg.list'
        apt-get update
        apt-get install -y postgresql-client-18
    else
        echo "Saltando instalación de PostgreSQL Client."
    fi
else
    echo "PostgreSQL client (pg_dump) ya está instalado. Omitiendo..."
fi

echo "[2/7] Verificando dependencias de bases de datos (MySQL/MariaDB Client)..."
if ! command -v mysqldump &> /dev/null; then
    echo "=========================================="
    echo " No se detectó 'mysqldump' en el sistema."
    echo " BackiieTUI lo requiere para respaldar MySQL o MariaDB."
    echo " ¿Deseas instalar el cliente estándar de MySQL/MariaDB?"
    echo " 1) Sí (Recomendado, instala default-mysql-client)"
    echo " 2) Saltar (Lo instalaré manualmente o no uso MySQL)"
    read -p "Selecciona una opción [1-2]: " mysql_opt

    if [ "$mysql_opt" == "1" ]; then
        echo "Instalando default-mysql-client..."
        apt-get update && apt-get install -y default-mysql-client || apt-get install -y mariadb-client
    else
        echo "Saltando instalación de MySQL Client."
    fi
else
    echo "MySQL client (mysqldump) ya está instalado. Omitiendo..."
fi

echo "[3/7] Verificando binario precompilado..."
if [ ! -f "backiietui_linux_amd64" ]; then
  echo "Error: No se encontro el binario 'backiietui_linux_amd64' en este directorio."
  exit 1
fi

echo "[4/7] Moviendo binarios a /usr/local/bin..."
cp backiietui_linux_amd64 /usr/local/bin/backiietui_bin
chmod +x /usr/local/bin/backiietui_bin

echo "[5/7] Creando wrapper inteligente 'backiie'..."
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

echo "[6/7] Creando directorio para la base de datos..."
mkdir -p /var/lib/backiie
chmod 777 /var/lib/backiie

echo "[7/7] Configurando el servicio de Systemd (Demonio de respaldos)..."
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
