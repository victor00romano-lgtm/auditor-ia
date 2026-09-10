package main

import (
	"context"
	"fmt"
	"log"
	"time"

	postgresdb "github.com/portfolio/auditor-ia/internal/platform/postgres"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := postgresdb.Open(ctx)
	if err != nil {
		log.Fatalf("abrir PostgreSQL: %v", err)
	}
	defer pool.Close()

	var version string
	if err := pool.QueryRow(ctx, "SELECT version()").Scan(&version); err != nil {
		log.Fatalf("consultar versão do PostgreSQL: %v", err)
	}

	fmt.Println("PostgreSQL conectado com sucesso!")
	fmt.Println(version)
}
