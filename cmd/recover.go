package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"BackiieTUI/application/usecases"
	"BackiieTUI/domain/entities"
)

func runRecover() {
	fmt.Println("=== Recuperación de Estado de BackiieTUI ===")
	fmt.Println("Este proceso descargará el respaldo más reciente de backiie.db desde S3.")
	fmt.Println("Advertencia: Esto SOBREESCRIBIRÁ tu base de datos local actual.")
	fmt.Print("¿Deseas continuar? (y/N): ")

	reader := bufio.NewReader(os.Stdin)
	ans, _ := reader.ReadString('\n')
	if strings.ToLower(strings.TrimSpace(ans)) != "y" {
		fmt.Println("Operación cancelada.")
		os.Exit(0)
	}

	endpoint := prompt(reader, "S3 Endpoint (ej. s3.amazonaws.com o localhost:9000): ")
	bucket := prompt(reader, "S3 Bucket: ")
	accessKey := prompt(reader, "Access Key ID: ")
	secretKey := prompt(reader, "Secret Access Key: ")
	pathPrefix := prompt(reader, "Path Prefix (opcional): ")
	hostname := prompt(reader, "Hostname original (dejar en blanco para usar el actual): ")

	s3cfg := &entities.S3Config{
		Endpoint:        endpoint,
		Bucket:          bucket,
		AccessKeyID:     accessKey,
		SecretAccessKey: secretKey,
		PathPrefix:      pathPrefix,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	dbPath := envOr("BACKIIE_DB_PATH", "backiie.db")

	uc := usecases.NewSelfRestoreUseCase(s3Factory{}, dbPath, logger)
	ctx := context.Background()

	fmt.Println("\nIniciando descarga...")
	if err := uc.Run(ctx, s3cfg, hostname); err != nil {
		fmt.Fprintf(os.Stderr, "Error durante la recuperación: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n¡Recuperación completada! Ahora puedes iniciar BackiieTUI normalmente.")
	os.Exit(0)
}

func prompt(r *bufio.Reader, msg string) string {
	fmt.Print(msg)
	ans, _ := r.ReadString('\n')
	return strings.TrimSpace(ans)
}
