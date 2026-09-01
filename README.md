# BackiieTUI

Gestor de respaldos de bases de datos con interfaz TUI (terminal), construido en Go.

Soporta **SQL Server 2022/2025**, **MySQL**, **MariaDB**, **PostgreSQL** y **Redis**. Los respaldos se suben a **S3** (o compatible: MinIO, Ceph, Hetzner). El historial se guarda en **BBolt** y los respaldos corren automáticamente según el cron configurado.

```
┌────────────────────────────────────────────────────────┐
│  Panel  │  Instancias  │  Respaldos  │  S3  │  Retención │
├────────────────────────────────────────────────────────┤
│  Instancias: 4          Respaldos totales: 28          │
│    SQL Server: 1          exitosos: 26  fallidos: 2    │
│    PostgreSQL: 2                                       │
│    Redis:      1        Próximos respaldos:            │
│                           Lun 31 Mar 00:00 UTC         │
└────────────────────────────────────────────────────────┘
```

---

## Requisitos

### Para compilar
- **Go 1.23+** — https://go.dev/dl

### En el servidor (solo los motores que uses)

| Motor | Herramienta requerida |
|---|---|
| PostgreSQL | `pg_dump` (`apt install postgresql-client`) |
| MySQL / MariaDB | `mysqldump` (`apt install mysql-client`) |
| SQL Server 2022+ | ninguna — backup nativo a S3 desde el propio servidor |
| Redis | ninguna — exporta vía cliente Go |

---

## Comandos Make

```bash
# Desarrollo
make build        # compilar para el OS actual
make run          # compilar y ejecutar
make vet          # verificar código
make clean        # eliminar binarios y backiie.db

# Distribución
make linux        # cross-compilar para Linux amd64 (x86_64)
make linux-arm64  # cross-compilar para Linux arm64 (Graviton / RPi)

# Servidor (ejecutar en el servidor Linux)
make install-service  # instalar como servicio systemd
make open-tui         # abrir la TUI (detiene el servicio temporalmente)
make logs             # ver logs en tiempo real
```

---

## Instalación como servicio en un servidor Linux

### Paso 1 — Compilar (desde tu máquina de desarrollo)

```bash
make linux
# Genera: backiietui-linux-amd64
```

### Paso 2 — Copiar al servidor

```bash
scp backiietui-linux-amd64 scripts/install.sh usuario@servidor:/tmp/
```

### Paso 3 — Instalar en el servidor

```bash
ssh usuario@servidor
cd /tmp

# Instalar dependencias de respaldo (según motores que uses)
sudo apt-get install -y postgresql-client mysql-client   # Debian/Ubuntu
# sudo yum install -y postgresql mysql                   # RHEL/CentOS

# Instalar el servicio (crea usuario, directorios, systemd y logrotate)
sudo bash install.sh
```

El script `install.sh` hace todo automáticamente:

| Qué crea | Dónde |
|---|---|
| Binario | `/usr/local/bin/backiietui` |
| Base de datos | `/var/lib/backiie/data.db` |
| Logs | `/var/log/backiietui/backiie.log` |
| Servicio systemd | `/etc/systemd/system/backiietui.service` |
| logrotate | `/etc/logrotate.d/backiietui` |
| Helper TUI | `/usr/local/bin/backiietui-tui` |

Si `tmux` está instalado, el servicio corre la TUI dentro de una sesión tmux (recomendado). Si no, corre en modo headless.

El servicio queda habilitado para **arrancar automáticamente con el servidor**.

---

## Abrir la TUI cuando el servicio está corriendo

La TUI y el daemon no pueden correr simultáneamente sobre el mismo archivo de datos (BBolt usa bloqueo exclusivo). El helper `backiietui-tui` detiene el servicio, abre la TUI con acceso completo a los datos, y al salir pregunta si reiniciar el servicio:

```bash
backiietui-tui
# (o desde el proyecto: make open-tui)
```

Flujo:
```
→ Deteniendo servicio backiietui...
→ Abriendo BackiieTUI...
  [ TUI interactiva con toda la configuración y historial ]
→ ¿Reiniciar el servicio backiietui? [S/n]
→ ✓ Servicio reiniciado.
```

---

## Monitoreo del servicio

### Ver logs en tiempo real

```bash
journalctl -u backiietui -f
# (o desde el proyecto: make logs)
```

### Comandos útiles de journalctl

```bash
journalctl -u backiietui -f               # logs en tiempo real
journalctl -u backiietui -n 100           # últimas 100 líneas
journalctl -u backiietui -p err           # solo errores
journalctl -u backiietui --since today    # desde hoy
journalctl -u backiietui --since "2025-08-01 00:00:00"
```

### Qué buscar en los logs

```
level=INFO  msg="modo servicio: TUI desactivada, scheduler activo"
            → Arrancó correctamente en modo daemon

level=INFO  msg="iniciando ciclo de respaldos programados"
            → El cron se disparó (cada cron configurado)

level=INFO  msg="backup completado"  instancia=prod-pg-01  bd=ventas  size_bytes=2048000  duration_ms=1823
            → Respaldo exitoso con tamaño y duración

level=ERROR msg="backup fallido"  instancia=prod-pg-01  bd=reportes  err="..."
            → Revisar el campo err para el detalle del problema

level=INFO  msg="retención diaria aplicada"  eliminados=3
            → Limpieza automática de backups expirados (1:00 AM)

level=INFO  msg="señal recibida, apagando"  señal=terminated
            → SIGTERM recibido, apagado limpio
```

### Estado y control del servicio

```bash
systemctl status backiietui       # estado actual
sudo systemctl restart backiietui # reiniciar
sudo systemctl stop backiietui    # detener
sudo systemctl start backiietui   # iniciar
```

---

## Primeros pasos (primera vez)

Abre la TUI con `backiietui-tui` (o detén el servicio y ejecuta `backiietui`).

### 1. Configurar S3 → pestaña **S3**

```
Bucket         mi-bucket-backups
Región         us-east-1
Endpoint       (vacío para AWS; http://minio:9000 para MinIO)
Access Key ID  AKIAIOSFODNN7EXAMPLE
Secret Key     ••••••••••••••••••••
Prefijo S3     backups/
```

Presiona **[ Probar Conexión ]** antes de guardar.

### 2. Agregar instancias → pestaña **Instancias** → tecla `n`

```
Nombre         prod-postgres-01
Motor          postgres
Host           192.168.1.10
Puerto         5432
Usuario        backup_user
Contraseña     ••••••••
```

Presiona `t` para probar la conexión.

### 3. Configurar retención → pestaña **Retención** → `[1]`

```
Días de retención global: 7
```

### 4. Configurar horario → pestaña **Retención** → `[2]`

```
Cron 1:  0 0 * * *    (todos los días a las 00:00)
Cron 2:  0 12 * * *   (todos los días a las 12:00)
Zona horaria: America/Bogota
```

### 5. Sincronizar lifecycle S3 → pestaña **Retención** → `[3]`

Presiona **`s`** para crear las reglas de ciclo de vida en el bucket S3.
Ver [`docs/s3-retencion.md`](docs/s3-retencion.md) para más detalles.

### 6. Respaldar manualmente

En **Instancias**, selecciona una instancia y presiona `b`.

---

## Teclas globales

| Tecla | Acción |
|---|---|
| `tab` / `shift+tab` | Cambiar pestaña |
| `n` | Nuevo elemento |
| `e` | Editar seleccionado |
| `d` | Eliminar seleccionado |
| `b` | Respaldar instancia ahora |
| `t` | Probar conexión |
| `r` / `f5` | Refrescar lista |
| `↑` / `k` | Subir |
| `↓` / `j` | Bajar |
| `enter` | Confirmar / ver detalle |
| `esc` | Cancelar / volver |
| `q` / `ctrl+c` | Salir |

En la pestaña **Respaldos**: `R` restaurar el seleccionado, `A` restaurar TODAS las bases SQL Server, `H` historial de restauraciones (todas piden escribir `confirmar`).

---

## Variables de entorno

| Variable | Descripción | Default |
|---|---|---|
| `BACKIIE_DB_PATH` | Ruta del archivo BBolt | `backiie.db` en el directorio actual |
| `BACKIIE_LOG_FILE` | Archivo de log adicional (además de journald) | desactivado |

---

## Registro de auditoría

Cada respaldo genera un registro con:

| Campo | Descripción |
|---|---|
| `id` | UUID v4 único |
| `instance_name` | instancia origen |
| `database_name` | base de datos respaldada |
| `file_name` | path completo en S3 |
| `engine` | motor utilizado |
| `size_bytes` | bytes subidos |
| `hash_sha256` | hash SHA-256 de integridad |
| `retain_days` | días de retención cuando se creó este respaldo |
| `status` | `running` / `completed` / `failed` / `expired` |
| `started_at` | timestamp de inicio |
| `completed_at` | timestamp de fin |
| `expires_at` | fecha de expiración calculada |
| `duration_ms` | duración en milisegundos |

Ver el detalle de cualquier respaldo: pestaña **Respaldos** → `enter`.

---

## SQL Server 2022/2025 — Full + Log nativos a S3, retención segura y restore

BackiieTUI crea la credencial S3 automáticamente y ejecuta los respaldos directamente en el servidor SQL — el archivo va del servidor SQL al bucket, sin pasar por la máquina donde corre BackiieTUI:

```sql
CREATE CREDENTIAL [s3://endpoint/bucket]
WITH IDENTITY = 'S3 Access Key', SECRET = 'accessKey:secretKey'

BACKUP DATABASE [nombre_bd] TO URL = 's3://bucket/path/backup.bak'
WITH FORMAT, COMPRESSION, STATS = 25, CHECKSUM
```

- **Full (`.bak`) + Log (`.trn`)**: por defecto, Full una vez al día y Log cada hora, encadenados. Incluye `master` y `msdb` además de tus bases de usuario. Configurable en **Retención → [2] Programador**.
- **Retención con regla de oro**: se conservan los últimos N días (default 3) de Full, pero nunca se borra uno si eso deja menos de 2 Fulls verificados en el bucket para esa base. Borrar un Full borra también sus `.trn` encadenados.
- **Restaurar**: `R` sobre un backup puntual (aplica la cadena Full+Logs automáticamente si es un Log), o `A` para **restaurar todas las bases de datos SQL Server** al último punto disponible de una sola vez. Ambas piden escribir `confirmar`. `H` muestra el historial de restauraciones.
- **Self-backup**: la propia base BBolt de BackiieTUI (instancias, historial) se sube a S3 cada hora, para poder reconstruir la configuración si se pierde el servidor.
- `master` requiere un procedimiento manual de restauración (single-user mode) — ver [`docs/operaciones.md`](docs/operaciones.md).

Detalle completo, incluyendo el requisito de certificado de CA pública en el endpoint S3 para que `BACKUP/RESTORE ... TO/FROM URL` funcione, en [`docs/operaciones.md`](docs/operaciones.md).

### Entorno de pruebas (SQL Server 2025 + MinIO)

```bash
make test              # unitarios, sin Docker
make test-env-up       # levanta SQL Server 2025 (17.x) + MinIO con TLS
make test-integration  # corre las pruebas de integración contra ese entorno
make test-env-down     # baja y limpia
```

---

## Documentación adicional

- [`docs/s3-retencion.md`](docs/s3-retencion.md) — cómo funcionan las lifecycle rules de S3 y cómo configurarlas (incluye Hetzner)
- [`docs/operaciones.md`](docs/operaciones.md) — monitoreo, logrotate, usuario del sistema, y limitaciones conocidas

---

## Arquitectura

```
cmd/main.go                 ← wiring + detección TTY (modo TUI vs modo servicio)
domain/entities/            ← structs sin dependencias externas
domain/ports/               ← interfaces (contratos)
application/usecases/       ← lógica de negocio
adapters/database/          ← conectores por motor
adapters/storage/s3/        ← cliente S3 con multipart upload y lifecycle rules
adapters/persistence/bbolt/ ← persistencia local
internal/scheduler/         ← cron scheduler (backups + limpieza diaria)
tui/                        ← interfaz Bubble Tea (5 pestañas)
scripts/                    ← install.sh y helpers
docs/                       ← documentación de S3 y operaciones
```
