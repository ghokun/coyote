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
	"strings"

	"github.com/fatih/color"
	"github.com/google/uuid"
	"github.com/manifoldco/promptui"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/urfave/cli/v2"
	_ "modernc.org/sqlite"
)

var Version = "development"

const usage = `coyote [global options]

Examples:
coyote --url amqps://user@myurl --exchange myexchange --store events.sqlite
coyote --url amqps://user:password@myurl --noprompt --exchange myexchange --store events.sqlite
coyote --url amqps://user:password@myurl --noprompt --insecure --exchange myexchange

Exchange binding formats:
 --exchange myexchange                            # All messages in single exchange
 --exchange myexchange1=mykey1                    # Messages with routing key in a single exchange
 --exchange myexchange1=mykey1,myexchange1=mykey2 # Messages with routing keys in a single exchange
 --exchange myexchange1,myexchange2               # All messages in multiple exchanges
 --exchange myexchange1=mykey1,myexchange2=mykey2 # Messages with routing keys in multiple exchanges
 --exchange myexchange1,myexchange2=mykey2        # Messages with or without routing keys in multiple exchanges`

type listen struct {
	c []combination
}

type combination struct {
	exchange   string
	routingKey string
}

func (l *listen) Set(value string) (err error) {
	for _, comb := range strings.Split(value, ",") {
		pair := strings.Split(comb, "=")
		length := len(pair)
		if length == 1 {
			if len(pair[0]) < 1 {
				return fmt.Errorf("exchange name can not be empty")
			}
			l.c = append(l.c, combination{exchange: pair[0], routingKey: "#"})
		} else if length == 2 {
			if len(pair[0]) < 1 {
				return fmt.Errorf("exchange name can not be empty")
			}
			if len(pair[1]) < 1 {
				return fmt.Errorf("routing key can not be empty when '=' is provided")
			}
			l.c = append(l.c, combination{exchange: pair[0], routingKey: pair[1]})
		} else {
			return fmt.Errorf("valid values are ['a=x' 'a,b' 'a=x,b=y' 'a,b=y'] where a and b are exchanges, x and y are routing keys")
		}
	}
	return nil
}

func (l *listen) String() string {
	return ""
}

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
			&cli.GenericFlag{
				Name:     "exchange",
				Required: true,
				Value:    &listen{},
				Usage:    "Exchange & routing key combinations to listen messages.",
			},
			&cli.StringFlag{
				Name:  "queue",
				Value: "interceptor",
				Usage: "Interceptor queue name.",
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

			q, err := ch.QueueDeclare(
				fmt.Sprintf("%s.%s", ctx.String("queue"), uuid.NewString()), // queue name
				false, // is durable
				true,  // is auto delete
				true,  // is exclusive
				false, // is no wait
				nil,   // args
			)
			if err != nil {
				return fmt.Errorf("%s %w", color.RedString("failed to declare a queue:"), err)
			}

			for _, c := range ctx.Generic("exchange").(*listen).c {
				err = ch.ExchangeDeclarePassive(
					c.exchange, // exchange name
					"topic",    // exchange kind
					true,       // is durable
					false,      // is auto delete
					false,      // is internal
					false,      // is no wait
					nil,        // args
				)
				if err != nil {
					return fmt.Errorf("%s %w", color.RedString("failed to connect to exchange:"), err)
				}

				err = ch.QueueBind(
					q.Name,       // interceptor queue name
					c.routingKey, // routing key to bind
					c.exchange,   // exchange to listen
					false,        // is no wait
					nil,          // args
				)
				if err != nil {
					return fmt.Errorf("%s %w", color.RedString("failed to bind to queue:"), err)
				} else {
					log.Printf("👂 Listening from exchange %s with routing key %s", color.YellowString(c.exchange), color.YellowString(c.routingKey))
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
					db, err = sql.Open("sqlite", filename)
					if err != nil {
						log.Fatal(err)
					}
					defer db.Close()
					statement, err := db.Prepare(`CREATE TABLE event 
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
					if _, err := statement.Exec(); err != nil {
						log.Fatal(err)
					}
					insert, err = db.Prepare(`INSERT INTO event(exchange, routing_key, correlation_id, reply_to, headers, body) 
					VALUES (?, ?, ?, ?, ?, ?)`)
					if err != nil {
						log.Fatal(err)
					}
				}
				for d := range deliveries {
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
					if insert != nil {
						if _, err := insert.Exec(d.Exchange, d.RoutingKey, d.CorrelationId, d.ReplyTo, fmt.Sprint(d.Headers), string(d.Body)); err != nil {
							log.Fatal(err)
						}
					}
				}
			}()

			log.Printf("⏳ Waiting for messages. To exit press %s", color.YellowString("CTRL+C"))
			<-ctx.Done()
			return nil
		},
	}

	if err := app.RunContext(ctx, os.Args); err != nil {
		log.Fatal(err)
	}
}
