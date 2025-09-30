package main

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/fatih/color"
	failed "github.com/ghokun/coyote/error"
	"github.com/google/uuid"
	"github.com/urfave/cli/v3"
	"log"
	_ "modernc.org/sqlite"
	"os"
	"os/signal"
)

var Version = "development"

const usage = `coyote [global options]

Examples:
# Store all messages from 'myexchange' into 'events.sqlite' file, prompting for password
coyote --url amqps://user@myurl --exchange myexchange=# --store events.sqlite

# Store all messages with routing key 'mykey' from 'myexchange' into events.sqlite file without prompting for password
coyote --url amqps://user:password@myurl --noprompt --exchange myexchange=mykey --store events.sqlite

# Capture all messages from 'myexchange' without certificate verification
coyote --url amqps://user:password@myurl --noprompt --insecure --exchange myexchange=#

Exchange binding formats:
 --exchange myexchange=#                          # All messages in single exchange
 --exchange myexchange1=mykey1                    # Messages with routing key in a single exchange
 --exchange myexchange1=mykey1,myexchange1=mykey2 # Messages with routing keys in a single exchange
 --exchange myexchange1=#,myexchange2=#           # All messages in multiple exchanges
 --exchange myexchange1=mykey1,myexchange2=mykey2 # Messages with routing keys in multiple exchanges
 --exchange myexchange1=#,myexchange2=mykey2      # Messages with or without specific routing keys in multiple exchanges`

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

	app := &cli.Command{
		Name:      "coyote",
		Usage:     "Coyote is a RabbitMQ message sink.",
		Version:   Version,
		UsageText: usage,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "url",
				Required: true,
				Usage:    "RabbitMQ url, must start with amqps:// or amqp://.",
			},
			&cli.BoolFlag{
				Name:  "oauth",
				Usage: "Use OAuth 2.0 for authentication.",
			},
			&cli.StringFlag{
				Name:  "redirect-url",
				Usage: "OIDC callback url for OAuth 2.0",
			},
			&cli.BoolFlag{
				Name:  "insecure",
				Usage: "Skips certificate verification.",
			},
			&cli.StringMapFlag{
				Name:        "exchange",
				Required:    true,
				Usage:       "Exchange & routing key combinations to listen messages.",
				DefaultText: "myexchange=#",
			},
			&cli.StringFlag{
				Name:  "queue",
				Usage: "Interceptor queue name. If provided, interceptor queue will not be auto deleted.",
			},
			&cli.StringFlag{
				Name:  "store",
				Usage: "SQLite filename to store events.",
			},
			&cli.BoolFlag{
				Name:  "silent",
				Usage: "Disables terminal print.",
			},
		},
		Action: func(ctx context.Context, cli *cli.Command) error {
			conn, err := connect(cli)
			if err != nil {
				return err
			}
			defer func() {
				err := conn.Close()
				if err != nil {
					log.Fatal(err)
				}
				log.Printf("⛓️‍💥 Terminating AMQP connection")
			}()

			ch, err := conn.Channel()
			if err != nil {
				return failed.Because("failed to open a channel:", err)
			}
			defer func() {
				err := ch.Close()
				if err != nil {
					log.Fatal(err)
				}
				log.Printf("⛓️‍💥 Terminating AMQP channel")
			}()

			var queueName string
			persistent := cli.IsSet("queue")
			if persistent {
				queueName = cli.String("queue")
			} else {
				queueName = fmt.Sprintf("%s.%s", "coyote", uuid.NewString())
			}
			q, err := ch.QueueDeclare(
				queueName,   // queue name
				false,       // is durable
				!persistent, // is auto delete
				!persistent, // is exclusive
				false,       // is no wait
				nil,         // args
			)
			if err != nil {
				return failed.Because("failed to declare a queue:", err)
			}

			for exchange, routingKey := range cli.StringMap("exchange") {
				err = ch.ExchangeDeclarePassive(
					exchange, // exchange name
					"topic",  // exchange kind
					true,     // is durable
					false,    // is auto delete
					false,    // is internal
					false,    // is no wait
					nil,      // args
				)
				if err != nil {
					return failed.Because("failed to connect to exchange:", err)
				}

				err = ch.QueueBind(
					q.Name,     // interceptor queue name
					routingKey, // routing key to bind
					exchange,   // exchange to listen
					false,      // is no wait
					nil,        // args
				)
				if err != nil {
					return failed.Because("failed to bind to queue:", err)
				} else {
					log.Printf("👂 Listening from exchange %s with routing key %s", color.YellowString(exchange), color.YellowString(routingKey))
				}
			}

			deliveries, err := ch.Consume(
				q.Name, // queue name to consume from
				"",     // consumer tag
				true,   // is auto ack
				false,  // is exclusive
				false,  // is no local
				false,  // is no wait
				nil,    // args
			)
			if err != nil {
				return failed.Because("failed to register a consumer:", err)
			}

			go func() {
				var db *sql.DB
				var insert *sql.Stmt
				if cli.IsSet("store") {
					filename := cli.String("store")
					db, err = sql.Open("sqlite", filename+"?_txlock=exclusive&mode=rwc")
					if err != nil {
						log.Fatal(err)
					}
					defer func() {
						err := db.Close()
						if err != nil {
							log.Fatal(err)
						}
						log.Printf("⛓️‍💥 Closing database connection")
					}()

					create, err := db.Prepare(`CREATE TABLE IF NOT EXISTS event 
					(
					  "id"             INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
					  "timestamp"      TIMESTAMP DEFAULT (DATETIME(CURRENT_TIMESTAMP, 'localtime')),
					  "exchange"       TEXT,
					  "routing_key"    TEXT,
					  "correlation_id" TEXT,
					  "reply_to"       TEXT,
					  "headers"        TEXT,
					  "body"           TEXT
					);`)
					if err != nil {
						log.Fatal(err)
					}
					if _, err := create.Exec(); err != nil {
						log.Fatal(err)
					}
					insert, err = db.Prepare(`INSERT INTO event(exchange, routing_key, correlation_id, reply_to, headers, body) 
					VALUES (?, ?, ?, ?, ?, ?)`)
					if err != nil {
						log.Fatal(err)
					}
				}
				count := 0
				for d := range deliveries {
					if insert != nil {
						if _, err := insert.Exec(d.Exchange, d.RoutingKey, d.CorrelationId, d.ReplyTo, fmt.Sprint(d.Headers), string(d.Body)); err != nil {
							log.Fatal(err)
						}
					}
					if !cli.Bool("silent") {
						log.Printf("📧 %s\n%s%s\n%s%s\n%s%s\n%s%s\n%s%s\n%s%s",
							color.YellowString("Received a message"),
							color.GreenString("# Exchange        : "),
							d.Exchange,
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
					} else {
						count++
						fmt.Printf("\033[1A\033[K")
						log.Printf("💾 Consumed %s messages. To exit press %s", color.GreenString("%d", count), color.YellowString("CTRL+C"))
					}
				}
			}()

			log.Printf("⏳ Waiting for messages. To exit press %s", color.YellowString("CTRL+C"))
			<-ctx.Done()
			return nil
		},
	}

	if err := app.Run(ctx, os.Args); err != nil {
		log.Fatal(err)
	}
}
