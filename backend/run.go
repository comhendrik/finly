package backend

import (
	"context"
	"database/sql"
	"log"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/gin-gonic/gin"
)

func Run() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" { log.Fatal("DATABASE_URL is required") }
	db, err := sql.Open("pgx", databaseURL)
	if err != nil { log.Fatalf("open PostgreSQL: %v", err) }
	defer db.Close()
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	for {
		if err := db.PingContext(ctx); err == nil { break }
		if ctx.Err() != nil { log.Fatalf("connect to PostgreSQL: %v", ctx.Err()) }
		time.Sleep(time.Second)
	}

	app := NewApp(db)
	app.secureCookie = os.Getenv("COOKIE_SECURE") != "false"
	if err := app.load(); err != nil {
		log.Fatalf("load data: %v", err)
	}

	gin.SetMode(gin.ReleaseMode)
	server := app.router()

	address := os.Getenv("ADDR")
	if address == "" {
		port := os.Getenv("PORT")
		if port == "" { port = "8080" }
		address = ":" + port
	}

	log.Printf("Finly listening on %s", address)
	if err := server.Run(address); err != nil {
		log.Fatalf("run server: %v", err)
	}
}
