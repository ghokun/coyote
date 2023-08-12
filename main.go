package main

import (
	"context"
	"crypto/tls"
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"

	"github.com/fatih/color"
	"github.com/google/uuid"
	"github.com/manifoldco/promptui"
	_ "github.com/mattn/go-sqlite3"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/urfave/cli/v2"
)

var Version = "development"

func main() {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)
	defer func() {
		signal.Stop(signalChan)
		cancel()
	}()

	go func() {
		select {
		case <-signalChan:
			cancel()
		case <-ctx.Done():
		}
		<-signalChan
		os.Exit(2)
	}()

	app := &cli.App{
		Name:    "coyote",
		Usage:   "Coyote is a RabbitMQ message sink.",
		Version: Version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "url",
				Required: true,
				Usage:    "RabbitMQ url, must start with amqps:// or amqp://.",
			},
			&cli.StringFlag{
				Name:     "exchange",
				Required: true,
				Usage:    "Exchange name to listen messages.",
			},
			&cli.StringFlag{
				Name:  "queue",
				Value: "interceptor",
				Usage: "Interceptor queue name.",
			},
			&cli.StringFlag{
				Name:  "bind",
				Value: "#",
				Usage: "Routing key to bind.",
			},
			&cli.StringFlag{
				Name:  "store",
				Usage: "SQLite filename to store events.",
			},
			&cli.BoolFlag{
				Name:  "insecure",
				Usage: "Skips certificate verification",
			},
			&cli.BoolFlag{
				Name:  "noprompt",
				Usage: "Disables password prompt",
			},
		},
		Action: func(ctx *cli.Context) error {
			u, err := url.Parse(ctx.String("url"))
			if err != nil {
				return fmt.Errorf("%s %w", color.RedString("failed to parse provided url:"), err)
			}

			if !ctx.Bool("noprompt") {
				prompt := promptui.Prompt{
					Label: "Password",
					Mask:  '*',
				}
				password, err := prompt.Run()
				if err != nil {
					return fmt.Errorf("%s %w", color.RedString("failed to provide password:"), err)
				}
				u.User = url.UserPassword(u.User.String(), password)
			}

			conn, err := amqp.DialTLS(u.String(), &tls.Config{InsecureSkipVerify: ctx.Bool("insecure")})
			if err != nil {
				return fmt.Errorf("%s %w", color.RedString("failed to connect to RabbitMQ:"), err)
			}
			defer conn.Close()

			ch, err := conn.Channel()
			if err != nil {
				return fmt.Errorf("%s %w", color.RedString("failed to open a channel:"), err)
			}
			defer ch.Close()

			err = ch.ExchangeDeclarePassive(ctx.String("exchange"), "topic", false, true, false, false, nil)
			if err != nil {
				return fmt.Errorf("%s %w", color.RedString("failed to connect to exchange:"), err)
			}

			q, err := ch.QueueDeclare(fmt.Sprintf("%s.%s", ctx.String("queue"), uuid.NewString()), false, true, false, false, nil)
			if err != nil {
				return fmt.Errorf("%s %w", color.RedString("failed to declare a queue:"), err)
			}

			err = ch.QueueBind(q.Name, ctx.String("bind"), ctx.String("exchange"), false, nil)
			if err != nil {
				return fmt.Errorf("%s %w", color.RedString("failed to bind to queue:"), err)
			}

			deliveries, err := ch.Consume(q.Name, "", true, false, false, false, nil)
			if err != nil {
				return fmt.Errorf("%s %w", color.RedString("failed to register a consumer:"), err)
			}

			go func() {
				var db *sql.DB
				var insert *sql.Stmt
				if ctx.IsSet("store") {
					filename := ctx.String("store")
					file, err := os.Create(filename)
					if err != nil {
						log.Fatal(err)
					}
					file.Close()
					db, err = sql.Open("sqlite3", filename)
					if err != nil {
						log.Fatal(err)
					}
					defer db.Close()
					statement, err := db.Prepare(`CREATE TABLE event 
					(
					  "id"             INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
					  "timestamp"      TIMESTAMP DEFAULT (DATETIME(CURRENT_TIMESTAMP, 'localtime')),
					  "routing_key"    TEXT,
					  "correlation_id" TEXT,
					  "reply_to"       TEXT,
					  "headers"        TEXT,
					  "body"           TEXT
					);`)
					if err != nil {
						log.Fatal(err)
					}
					if _, err := statement.Exec(); err != nil {
						log.Fatal(err)
					}
					insert, err = db.Prepare(`INSERT INTO event(routing_key, correlation_id, reply_to, headers, body) 
					VALUES (?, ?, ?, ?, ?)`)
					if err != nil {
						log.Fatal(err)
					}
				}
				for d := range deliveries {
					log.Printf("📧 %s\n%s%s\n%s%s\n%s%s\n%s%s\n%s%s",
						color.YellowString("Received a message"),
						color.GreenString("# Routing-key     : "),
						d.RoutingKey,
						color.GreenString("# Correlation-id  : "),
						d.CorrelationId,
						color.GreenString("# Reply-to        : "),
						d.ReplyTo,
						color.GreenString("# Headers         : "),
						d.Headers,
						color.GreenString("# Body            : "),
						d.Body)
					if insert != nil {
						if _, err := insert.Exec(d.RoutingKey, d.CorrelationId, d.ReplyTo, fmt.Sprint(d.Headers), string(d.Body)); err != nil {
							log.Fatal(err)
						}
					}
				}
			}()

			log.Printf("⏳ Waiting for messages. To exit press CTRL+C")
			<-ctx.Done()
			return nil
		},
	}

	if err := app.RunContext(ctx, os.Args); err != nil {
		log.Fatal(err)
	}
}
