#!/usr/bin/env bash
# install.sh — Instala BackiieTUI como servicio systemd en Linux.
# Ejecutar como root o con sudo: sudo bash install.sh
set -euo pipefail

BINARY=backiietui
INSTALL_BIN=/usr/local/bin/$BINARY
SERVICE_USER=backiie
DB_PATH=/var/lib/backiie/data.db
LOG_DIR=/var/log/backiietui
LOG_FILE=$LOG_DIR/backiie.log
SERVICE_FILE=/etc/systemd/system/$BINARY.service
LOGROTATE_FILE=/etc/logrotate.d/$BINARY
OPEN_TUI_SCRIPT=/usr/local/bin/backiietui-tui

# ── Verificar que corremos como root ──────────────────────────────────────────
if [[ $EUID -ne 0 ]]; then
    echo "Error: ejecuta con sudo: sudo bash install.sh" >&2
    exit 1
fi

# ── Buscar el binario compilado ───────────────────────────────────────────────
BINARY_SRC=""
for candidate in "./backiietui-linux-amd64" "./backiietui-linux-arm64" "./$BINARY"; do
    if [[ -f "$candidate" ]]; then
        BINARY_SRC="$candidate"
        break
    fi
done
if [[ -z "$BINARY_SRC" ]]; then
    echo "Error: no se encontró el binario." >&2
    echo "  Ejecuta 'make linux' primero y copia el archivo aquí." >&2
    exit 1
fi

echo "Usando binario: $BINARY_SRC"

# ── Crear usuario del sistema ─────────────────────────────────────────────────
if ! id "$SERVICE_USER" &>/dev/null; then
    useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER"
    echo "✓ Usuario $SERVICE_USER creado"
else
    echo "  Usuario $SERVICE_USER ya existe"
fi

# ── Crear directorios ─────────────────────────────────────────────────────────
mkdir -p /var/lib/backiie "$LOG_DIR"
chown "$SERVICE_USER:$SERVICE_USER" /var/lib/backiie "$LOG_DIR"
echo "✓ Directorios en /var/lib/backiie y $LOG_DIR"

# ── Instalar binario ──────────────────────────────────────────────────────────
install -m 755 "$BINARY_SRC" "$INSTALL_BIN"
echo "✓ Binario en $INSTALL_BIN"

# ── Instalar script helper para abrir la TUI ─────────────────────────────────
cat > "$OPEN_TUI_SCRIPT" << 'SCRIPT'
#!/usr/bin/env bash
# backiietui-tui — Abre la TUI interactiva deteniendo el servicio temporalmente.
SERVICE=backiietui
DB=/var/lib/backiie/data.db

was_active=false
if systemctl is-active --quiet "$SERVICE" 2>/dev/null; then
    was_active=true
    echo "Deteniendo el servicio $SERVICE..."
    sudo systemctl stop "$SERVICE"
fi

echo "Abriendo BackiieTUI (la TUI tiene acceso completo)..."
echo ""
BACKIIE_DB_PATH="$DB" /usr/local/bin/backiietui

echo ""
if $was_active; then
    read -rp "¿Reiniciar el servicio $SERVICE? [S/n] " resp
    resp="${resp:-s}"
    if [[ "$resp" =~ ^[Ss]$ ]]; then
        sudo systemctl start "$SERVICE"
        echo "✓ Servicio reiniciado."
    else
        echo "Servicio no reiniciado. Para iniciarlo: sudo systemctl start $SERVICE"
    fi
fi
SCRIPT
chmod +x "$OPEN_TUI_SCRIPT"
echo "✓ Helper backiietui-tui en $OPEN_TUI_SCRIPT"

# ── Escribir servicio systemd (siempre Type=simple / headless) ────────────────
# Los usuarios de sistema con shell nologin no son compatibles con tmux.
# Para abrir la TUI interactiva usa: backiietui-tui
cat > "$SERVICE_FILE" << EOF
[Unit]
Description=BackiieTUI Database Backup Manager
After=network.target

[Service]
Type=simple
User=$SERVICE_USER
Environment=BACKIIE_DB_PATH=$DB_PATH
Environment=BACKIIE_LOG_FILE=$LOG_FILE
ExecStart=$INSTALL_BIN
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF
echo "✓ Servicio configurado en modo headless (scheduler activo)"
echo "  Para abrir la TUI interactiva: backiietui-tui"

# ── logrotate ─────────────────────────────────────────────────────────────────
cat > "$LOGROTATE_FILE" << EOF
$LOG_FILE {
    daily
    rotate 30
    compress
    delaycompress
    missingok
    notifempty
    create 0640 $SERVICE_USER $SERVICE_USER
    postrotate
        systemctl kill -s HUP $BINARY 2>/dev/null || true
    endscript
}
EOF
echo "✓ logrotate configurado"

# ── Activar e iniciar el servicio ─────────────────────────────────────────────
systemctl daemon-reload
systemctl enable "$BINARY"
systemctl start "$BINARY"

echo ""
echo "═══════════════════════════════════════════════════════"
echo "  ✓ BackiieTUI instalado y activo como servicio"
echo ""
echo "  Estado:         systemctl status $BINARY"
echo "  Logs en vivo:   journalctl -u $BINARY -f"
echo "  Abrir la TUI:   backiietui-tui"
echo "═══════════════════════════════════════════════════════"
