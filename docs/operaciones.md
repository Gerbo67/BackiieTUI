# Operaciones y monitoreo del servicio

## ¿Cómo funciona el servicio en Linux?

BackiieTUI tiene dos modos de ejecución que detecta automáticamente:

| Condición | Modo |
|---|---|
| stdout es una terminal (TTY) | **Interactivo** — renderiza la TUI |
| stdout NO es TTY (systemd, pipe, nohup) | **Servicio** — scheduler activo, sin TUI |

Cuando corre como servicio no hay interfaz visible. Los respaldos se ejecutan según el cron configurado y todos los eventos se registran en el log.

---

## Crear el usuario del servicio

El servicio corre bajo el usuario `backiie` (sistema, sin shell, sin directorio home):

```bash
sudo useradd --system --no-create-home --shell /usr/sbin/nologin backiie

# Crear directorio de datos
sudo mkdir -p /var/lib/backiie
sudo chown backiie:backiie /var/lib/backiie

# Crear directorio de logs (si usas BACKIIE_LOG_FILE)
sudo mkdir -p /var/log/backiietui
sudo chown backiie:backiie /var/log/backiietui
```

---

## Instalar como servicio systemd

```ini
# /etc/systemd/system/backiietui.service
[Unit]
Description=BackiieTUI Database Backup Manager
After=network.target

[Service]
Type=simple
User=backiie
Environment=BACKIIE_DB_PATH=/var/lib/backiie/data.db
Environment=BACKIIE_LOG_FILE=/var/log/backiietui/backiie.log
ExecStart=/usr/local/bin/backiietui
Restart=on-failure
RestartSec=10
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now backiietui
```

---

## Variables de entorno

| Variable | Descripción | Default |
|---|---|---|
| `BACKIIE_DB_PATH` | Ruta del archivo BBolt | `backiie.db` (directorio actual) |
| `BACKIIE_LOG_FILE` | Ruta del archivo de log adicional | (vacío = solo journald) |

El log escribe en **dos destinos simultáneamente** cuando `BACKIIE_LOG_FILE` está definido:
- `stderr` → capturado por journald (siempre)
- `BACKIIE_LOG_FILE` → archivo en disco (si está configurado)

---

## Ver los logs del servicio

### Con journalctl (recomendado)

```bash
# Todos los logs del servicio
journalctl -u backiietui

# Seguir en tiempo real (como tail -f)
journalctl -u backiietui -f

# Últimas 100 líneas
journalctl -u backiietui -n 100

# Solo errores
journalctl -u backiietui -p err

# Desde una fecha específica
journalctl -u backiietui --since "2025-08-15 00:00:00"

# Del día de hoy
journalctl -u backiietui --since today

# Formato JSON (para parsear con jq)
journalctl -u backiietui -o json | jq '.MESSAGE'
```

### Con el archivo de log (si configuraste BACKIIE_LOG_FILE)

```bash
# Seguir el archivo
tail -f /var/log/backiietui/backiie.log

# Buscar errores
grep '"level":"ERROR"' /var/log/backiietui/backiie.log

# Buscar un respaldo específico
grep '"bd":"ventas"' /var/log/backiietui/backiie.log
```

---

## Formato de las líneas de log

BackiieTUI usa `log/slog` en formato texto estructurado:

```
time=2025-08-15T01:00:00.000Z level=INFO msg="iniciando ciclo de respaldos programados"
time=2025-08-15T01:00:01.000Z level=INFO msg="backup completado" instancia=prod-pg-01 bd=ventas size_bytes=2048000 duration_ms=1823
time=2025-08-15T01:00:05.000Z level=ERROR msg="backup fallido" instancia=prod-pg-01 bd=reportes motor=PostgreSQL err="pg_dump: error: connection to server..."
time=2025-08-15T01:00:10.000Z level=INFO msg="retención diaria aplicada" eliminados=3
```

Mensajes clave:

| Mensaje | Significado |
|---|---|
| `modo servicio: TUI desactivada` | Arrancó sin TTY, modo daemon activo |
| `iniciando ciclo de respaldos programados` | El cron se disparó |
| `backup completado` | Respaldo exitoso |
| `backup fallido` | Error en un respaldo (ver campo `err`) |
| `retención aplicada` / `retención diaria aplicada` | Limpieza de backups expirados |
| `señal recibida, apagando` | SIGTERM recibido, apagado limpio |

---

## Rotar el archivo de log con logrotate

Si usas `BACKIIE_LOG_FILE`, configura logrotate para que no crezca indefinidamente:

```
# /etc/logrotate.d/backiietui
/var/log/backiietui/backiie.log {
    daily
    rotate 30
    compress
    delaycompress
    missingok
    notifempty
    create 0640 backiie backiie
    postrotate
        systemctl kill -s HUP backiietui 2>/dev/null || true
    endscript
}
```

> Si prefieres no usar archivo de log, omite `BACKIIE_LOG_FILE` y usa solo `journalctl`. journald gestiona el tamaño automáticamente.

---

## Estado del servicio

```bash
# Estado general
systemctl status backiietui

# ¿Está activo?
systemctl is-active backiietui

# Reiniciar
sudo systemctl restart backiietui

# Detener
sudo systemctl stop backiietui
```

---

## Abrir la TUI interactiva en un servidor con el servicio activo

El servicio corre en background. Para ver la interfaz:

```bash
# Con screen
screen -S backiie
backiietui
# Ctrl+A, D para detach (el servicio sigue corriendo en background)

# Con tmux
tmux new -s backiie
backiietui
# Ctrl+B, D para detach
```

> No ejecutes una segunda instancia si el servicio ya está corriendo — ambas compartirían el mismo archivo BBolt y podría corromperse. Detén el servicio primero (`systemctl stop backiietui`) antes de abrir la TUI manualmente.

---

## ¿Qué NO guarda en BBolt los logs?

BBolt es la base de datos de **auditoría de respaldos** (qué se respaldó, cuándo, resultado). No se usa para logs del sistema porque:

- BBolt no libera espacio en disco cuando eliminas registros (el archivo crece pero no se encoge hasta que se compacta).
- Los errores del sistema operativo (conexión, red, permisos) se manejan mucho mejor con journald/archivos de texto, que tienen rotación nativa.

El archivo `data.db` solo crece cuando hay nuevos registros de respaldo. Con 10 instancias y 2 respaldos/día cada una durante 7 días = ~140 registros por ciclo completo. El tamaño del archivo BBolt se mantiene pequeño y estable una vez que la retención empieza a limpiar registros expirados.

---

## Cambio de configuración S3 en vivo

Si cambias la configuración S3 desde la TUI mientras el servicio está corriendo, el scheduler usará los nuevos valores automáticamente en el siguiente ciclo de respaldos — no es necesario reiniciar el servicio.

---

## SQL Server: estrategia Full + Log y restauración

Para instancias SQL Server (2022+/2025), BackiieTUI respalda:

- **Full (`.bak`)**: todas las bases de datos, incluidas `master` y `msdb` (excluidas por defecto sólo `tempdb` y `model`). Por defecto, una vez al día.
- **Log (`.trn`)**: encadenado al Full más reciente completado de cada base. Por defecto, cada hora. Se omite automáticamente (no cuenta como error) en bases con recovery model `SIMPLE` — típicamente `master` y, salvo que lo hayas cambiado, `msdb`.

Ambas cadencias se configuran en la pestaña **Retención → [2] Programador** (formato cron con segundos: `segundo minuto hora día_mes mes día_semana`).

### Retención (regla de oro)

Se conservan los últimos `N` días de Full (default 3), pero **nunca se borra un Full si eso deja menos de 2 Fulls verificados** (confirmados presentes en el bucket) para esa base de datos. Al borrar un Full se borran también todos sus `.trn` encadenados. Esto corre una vez al día junto con la retención de los demás motores.

### Restaurar

Desde la pestaña **Respaldos**:
- `R` sobre un backup puntual (Full o Log) — si es un Log, se aplican automáticamente el Full padre y todos los Logs intermedios en orden.
- `A` — **restaurar TODAS las bases de datos SQL Server** al último punto de recuperación disponible, en el mismo lugar. Útil para reconstruir un servidor completo backup por backup sin hacerlo uno por uno.
- Ambas acciones piden escribir la palabra `confirmar` antes de ejecutar.
- `H` muestra el historial de restauraciones ejecutadas (qué se restauró, cuándo, con qué backups en la cadena).

### `master` requiere un procedimiento manual

`master` se respalda igual que cualquier otra base (para tener el `.bak` disponible en un desastre total), pero **queda excluida del restore automático** (`R` sobre su propio backup y del botón `A`). Restaurar `master` exige parar el servicio de SQL Server y arrancarlo en modo single-user, algo que no se puede hacer de forma segura ni portable (systemd vs Docker vs Windows) desde una conexión SQL normal. Procedimiento manual:

```bash
# 1. Detener SQL Server
sudo systemctl stop mssql-server   # o el equivalente en tu despliegue

# 2. Arrancar en modo single-user apuntando a master
sudo -u mssql /opt/mssql/bin/sqlservr -m -T3608 &

# 3. Conectarte con sqlcmd y restaurar desde el mismo bucket/prefijo que usa BackiieTUI
sqlcmd -S localhost -U sa -Q "
RESTORE DATABASE master FROM URL = 's3://<endpoint>/<bucket>/<prefix>/sqlserver/<instancia>/master/master_<timestamp>.bak'
WITH REPLACE, STATS = 10"

# 4. Detener el proceso en modo single-user y reiniciar el servicio normalmente
sudo systemctl start mssql-server
```

El nombre exacto del archivo `.bak` de `master` se ve en la pestaña **Respaldos** filtrando por esa base de datos.

### Ajustes de conexión

- `trustservercertificate=true` va por defecto en el DSN (los despliegues on-prem/Docker usan el certificado autofirmado de SQL Server). Desactivable por instancia con `Instance.Extra["trust_cert"] = "false"`.
- El timeout de conexión (que en el driver también actúa como deadline de inactividad para *toda* la conexión, no sólo el login) es de 300s por defecto — generoso a propósito, porque `BACKUP/RESTORE ... TO/FROM URL` puede quedarse en silencio un rato hablando con S3 entre mensajes de progreso. Ajustable por instancia con `Instance.Extra["conn_timeout_seconds"]` si respaldas bases muy grandes.

### El endpoint S3 debe tener un certificado de una CA pública

Confirmado probando contra un MinIO local: `BACKUP/RESTORE ... TO/FROM URL` de SQL Server exige que el endpoint S3 presente un certificado que encadene a una CA pública reconocida. Un certificado autofirmado **no funciona aunque lo agregues al almacén de confianza del sistema operativo y `openssl` lo valide correctamente ahí** — el cliente S3 interno de SQL Server usa su propio almacén de confianza, no el del SO. Con **Cloudflare R2** (el destino que se usa en este proyecto) esto no es un problema, ya que su certificado ya es de una CA pública. Si en algún momento apuntas a un MinIO/Ceph self-hosted en vez de R2, ese endpoint necesita un certificado real (por ejemplo, un MinIO detrás de un proxy con Let's Encrypt) — uno autofirmado no es suficiente para este feature en particular, aunque sí sirve para el resto de BackiieTUI (el adaptador S3 genérico usado por otros motores y por el self-backup si usa el SDK de Go, que sí respeta el almacén de confianza del SO).

---

## Self-backup de BackiieTUI

Cada hora, BackiieTUI sube una copia en caliente de su propia base BBolt (instancias, historial de respaldos y de restauraciones) a `S3://<bucket>/<prefix>/_backiie-meta/<hostname>/backiie_<timestamp>.db`. Si el servidor que corre BackiieTUI se pierde por completo, instala BackiieTUI en uno nuevo, descarga el `.db` más reciente de ese prefijo, colócalo en `BACKIIE_DB_PATH` y arranca el servicio: recupera toda la configuración (instancias, S3, retención, programación) sin tener que reconfigurar nada a mano.
