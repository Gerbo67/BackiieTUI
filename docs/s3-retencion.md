# S3 y Políticas de Retención en BackiieTUI

## ¿Qué es S3?

S3 (Simple Storage Service) es un sistema de almacenamiento de objetos. Un **objeto** es cualquier archivo junto con sus metadatos; se guarda en un **bucket** (contenedor) identificado por una clave (ruta/nombre). Es compatible con AWS S3, MinIO, Ceph, Hetzner Object Storage y otros proveedores.

---

## ¿Quién controla cuánto tiempo se guarda un backup?

**BackiieTUI y S3 son dos sistemas independientes.** Cada uno tiene su propia forma de manejar la retención. BackiieTUI usa los **dos** de forma complementaria.

### Mecanismo 1 — Retención gestionada por la app (principal)

Cuando BackiieTUI sube un backup a S3, registra en BBolt:

```
ExpiresAt  = fecha_actual + RetainDays
RetainDays = días configurados en la política activa para esa instancia
```

El scheduler corre diariamente a la 1:00 AM y también al iniciar la app:
1. Busca todos los registros con `ExpiresAt < ahora`.
2. Llama a `DeleteObject` en S3 para borrar el archivo.
3. Marca el registro como `expired`.

**Este mecanismo funciona con cualquier proveedor S3-compatible.**

---

### Mecanismo 2 — Lifecycle Rules en S3 (red de seguridad + Hetzner)

S3 puede eliminar objetos automáticamente según reglas configuradas en el bucket.

**Las reglas de lifecycle se configuran en el bucket**, no en la subida del archivo.

**BackiieTUI gestiona estas reglas directamente** desde la pestaña **Retención → [3] Lifecycle S3**. No necesitas ir a la consola de AWS ni a la CLI — el propio programa crea, consulta y elimina las reglas.

> **Nota Hetzner:** Hetzner Object Storage no tiene interfaz gráfica para lifecycle rules. La integración de BackiieTUI es la única forma de gestionar estas reglas sin usar la API manualmente.

---

## Estructura de las claves S3

BackiieTUI guarda los backups con este formato de ruta:

```
{PathPrefix}/{motor}/{instancia}/{base_de_datos}_{fecha}.ext
```

Ejemplos con PathPrefix = `backups`:

```
backups/postgres/prod-pg-01/ventas_20250815_000001.sql.gz
backups/mysql/tienda-01/productos_20250815_000001.sql.gz
backups/sqlserver/erp-main/Contabilidad_20250815_000001.bak
backups/redis/cache-01/db0_20250815_000001.json.gz
```

Esta estructura permite crear reglas de ciclo de vida por instancia usando prefijos de ruta.

---

## Cómo funciona la sincronización de lifecycle

Cuando presionas **[s] Sincronizar con retención** en la pestaña Lifecycle S3:

1. Lee las políticas de retención configuradas.
2. Preserva las reglas que NO son de BackiieTUI (no toca las reglas de otros sistemas).
3. Reemplaza las reglas `backiie-*` con las nuevas basadas en la configuración actual:

**Ejemplo con:**
- Global: 7 días
- Instancia `prod-pg-01` (PostgreSQL): 3 días personalizado

Crea:
```
ID: backiie-prod-pg-01   Prefijo: backups/postgres/prod-pg-01/   Días: 3
ID: backiie-global       Prefijo: backups/                        Días: 7
```

- Los backups de `prod-pg-01` expiran a los 3 días (por la regla de prefijo específica).
- Los demás backups expiran a los 7 días (por la regla global).
- Las reglas de otros sistemas no se modifican.

---

## ¿Los días de retención en BackiieTUI y en S3 deben ser iguales?

**Sí, la sincronización los pone iguales.** Ambos mecanismos actúan de forma independiente:

- BackiieTUI borra el objeto a las 1:00 AM cuando `ExpiresAt < ahora`.
- La regla S3 puede borrar el objeto en cualquier momento del mismo día o el siguiente.

Quien llega primero lo borra. Si BackiieTUI ya lo borró, S3 simplemente no encuentra el objeto y no hace nada. No hay error.

---

## Configuración recomendada

```
Prefijo S3 (PathPrefix):  backups/    ← siempre usar un prefijo
Días global:              7
```

> Si dejas `PathPrefix` vacío, la regla `backiie-global` aplica a **todo el bucket**. Solo es seguro si el bucket es exclusivo para backups.

---

## Reglas gestionadas por BackiieTUI vs externas

En la vista **[3] Lifecycle S3**, la columna "Origen" indica:

| Origen | Descripción |
|---|---|
| **BackiieTUI** | Regla creada y gestionada por esta app (ID empieza con `backiie-`) |
| externo | Regla creada por otro sistema — BackiieTUI no la modifica |

---

## ¿Qué pasa si cambio los días de retención?

| Situación | Comportamiento |
|---|---|
| Cambias días en Retención → vas a Lifecycle → presionas `s` | S3 actualiza las reglas automáticamente |
| Cambias días en Retención pero NO sincronizas | Las reglas S3 siguen con los días anteriores hasta que sincronices |
| Eliminas una regla `backiie-*` manualmente | En el próximo sync se vuelve a crear |

---

## Resumen visual

```
┌─────────────────────────────────────────────────────────────────┐
│                        ¿Quién borra?                            │
├──────────────────────┬──────────────────────────────────────────┤
│ BackiieTUI           │ Borra según ExpiresAt exacto por BD      │
│ (principal)          │ Funciona con AWS, MinIO, Hetzner, Ceph   │
│                      │ Se ejecuta: al iniciar + diario 1:00 AM  │
├──────────────────────┼──────────────────────────────────────────┤
│ S3 Lifecycle Rule    │ Borra según edad del objeto (días)       │
│ (red de seguridad)   │ Gestionado desde pestaña Lifecycle S3    │
│                      │ Sincronizar con [s] para mantener alineado│
└──────────────────────┴──────────────────────────────────────────┘
```
