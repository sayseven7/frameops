package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sayseven7/frameops/internal/store/postgres"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "bootstrap-first-admin" {
		fmt.Fprintln(os.Stderr, "usage: fops bootstrap-first-admin --token-file PATH --password-file PATH --organization NAME --name NAME --email EMAIL")
		os.Exit(2)
	}
	flags := flag.NewFlagSet("bootstrap-first-admin", flag.ExitOnError)
	tokenFile := flags.String("token-file", "", "owner-only bootstrap token file")
	passwordFile := flags.String("password-file", "", "local password file")
	organization := flags.String("organization", "", "organization name")
	name := flags.String("name", "", "admin display name")
	email := flags.String("email", "", "admin email")
	_ = flags.Parse(os.Args[2:])
	password, err := os.ReadFile(*passwordFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read password file:", err)
		os.Exit(1)
	}
	databaseURL := os.Getenv("FRAMEOPS_DATABASE_URL")
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "FRAMEOPS_DATABASE_URL must be set explicitly")
		os.Exit(1)
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer pool.Close()
	if _, err := postgres.BootstrapFirstAdmin(context.Background(), pool, postgres.BootstrapInput{OrganizationName: *organization, DisplayName: *name, Email: *email, Password: strings.TrimSpace(string(password)), TokenFile: *tokenFile}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
