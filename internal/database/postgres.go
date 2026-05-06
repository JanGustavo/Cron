package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq"
)

// Válida a conexão com o PostgreSQL e retorna um *sql.DB configurado para uso.
func ConnectPostgres(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("falha ao abrir conexão com PostgreSQL: %w", err)
	}

	db.SetMaxOpenConns(25)                 // Máximo de conexões abertas
	db.SetMaxIdleConns(25)                 // Conexões mantidas ociosas
	db.SetConnMaxLifetime(5 * time.Minute) // Tempo máximo de vida da conexão

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("falha ao pingar PostgreSQL: %w", err)
	}

	log.Println("Conexão com PostgreSQL estabelecida com sucesso")
	return db, nil
}
