BINARY     := backiietui
CMD        := ./cmd/main.go
LDFLAGS    := -s -w
GOFLAGS    := -trimpath

.PHONY: build run linux linux-arm64 install install-service open-tui logs tidy vet clean help test test-env-up test-env-down test-integration

help:
	@echo ""
	@echo "BackiieTUI — Comandos disponibles:"
	@echo ""
	@echo "  Desarrollo:"
	@echo "    make build        Compilar binario para el OS actual"
	@echo "    make run          Compilar y ejecutar"
	@echo "    make vet          Verificar código con go vet"
	@echo "    make tidy         go mod tidy"
	@echo "    make clean        Eliminar binarios y backiie.db local"
	@echo "    make test         Correr pruebas unitarias (sin Docker)"
	@echo ""
	@echo "  Pruebas de integración (requieren Docker):"
	@echo "    make test-env-up      Levantar SQL Server 2025 + MinIO de prueba"
	@echo "    make test-integration Correr pruebas de integración contra ese entorno"
	@echo "    make test-env-down    Bajar y limpiar el entorno de prueba"
	@echo ""
	@echo "  Distribución:"
	@echo "    make linux        Cross-compilar para Linux amd64  (prod / x86_64)"
	@echo "    make linux-arm64  Cross-compilar para Linux arm64  (Graviton / RPi)"
	@echo ""
	@echo "  Servidor Linux (ejecutar en el servidor):"
	@echo "    make install-service  Instalar como servicio systemd (requiere sudo)"
	@echo "    make open-tui         Abrir la TUI interactiva (detiene el servicio)"
	@echo "    make logs             Ver logs en tiempo real (journalctl -f)"
	@echo ""

build:
	go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY) $(CMD)

run: build
	./$(BINARY)

linux:
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY)-linux-amd64 $(CMD)
	@echo "Binario listo: $(BINARY)-linux-amd64"

linux-arm64:
	GOOS=linux GOARCH=arm64 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BINARY)-linux-arm64 $(CMD)
	@echo "Binario listo: $(BINARY)-linux-arm64"

# Instala como servicio systemd. Ejecutar directamente en el servidor Linux.
install-service:
	@if [ ! -f scripts/install.sh ]; then echo "Error: ejecuta desde el directorio del proyecto."; exit 1; fi
	sudo bash scripts/install.sh

# Abre la TUI interactiva. Detiene el servicio mientras la TUI está abierta.
open-tui:
	@command -v backiietui-tui >/dev/null 2>&1 || { echo "Ejecuta 'make install-service' primero."; exit 1; }
	backiietui-tui

# Ver logs en tiempo real (requiere systemd).
logs:
	journalctl -u $(BINARY) -f

tidy:
	go mod tidy

vet:
	go vet ./...

test:
	go test ./...

test-env-up:
	docker compose -f docker-compose.test.yml up -d mssql minio minio-init --wait

test-env-down:
	docker compose -f docker-compose.test.yml down -v

# Corre dentro de la red de docker-compose (ver comentario en el servicio "runner"), para que
# SQL Server y el test vean el mismo endpoint S3.
test-integration:
	docker compose -f docker-compose.test.yml run --rm runner \
		go test -tags integration -timeout 10m ./test/...

clean:
	rm -f $(BINARY) $(BINARY)-linux-amd64 $(BINARY)-linux-arm64 backiie.db
