package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	_ "github.com/microsoft/go-mssqldb"
)

func main() {
	connString := "sqlserver://sa:Password123!@localhost:1433?connection+timeout=5&trustservercertificate=true"
	db, err := sql.Open("sqlserver", connString)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// 0: current log, 1: sql error log
	rows, err := db.QueryContext(context.Background(), "EXEC sp_readerrorlog 0, 1")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var logDate string
		var processInfo string
		var text string
		if err := rows.Scan(&logDate, &processInfo, &text); err != nil {
			continue
		}
		fmt.Printf("%s: %s\n", logDate, text)
	}
}
