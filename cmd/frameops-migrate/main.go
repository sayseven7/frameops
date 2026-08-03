package main

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

const migrationsDirectory = "migrations"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) (runErr error) {
	if len(args) == 0 {
		return fmt.Errorf("usage: frameops-migrate {up|status|down-to VERSION}")
	}

	databaseURL := os.Getenv("FRAMEOPS_DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("FRAMEOPS_DATABASE_URL must be set explicitly")
	}

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}

	database, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if err := database.Close(); err != nil && runErr == nil {
			runErr = fmt.Errorf("close database: %w", err)
		}
	}()

	if err := database.Ping(); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	switch args[0] {
	case "up":
		if len(args) != 1 {
			return fmt.Errorf("usage: frameops-migrate up")
		}
		return goose.Up(database, migrationsDirectory)
	case "status":
		if len(args) != 1 {
			return fmt.Errorf("usage: frameops-migrate status")
		}
		return goose.Status(database, migrationsDirectory)
	case "down-to":
		if len(args) != 2 {
			return fmt.Errorf("usage: frameops-migrate down-to VERSION")
		}
		version, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil || version < 0 {
			return fmt.Errorf("migration version must be a non-negative integer")
		}
		return goose.DownTo(database, migrationsDirectory, version)
	default:
		return fmt.Errorf("usage: frameops-migrate {up|status|down-to VERSION}")
	}
}
