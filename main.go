package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"

	"github.com/google/uuid"
	"github.com/manifoldco/promptui"
	amqp "github.com/rabbitmq/amqp091-go"
	cli "github.com/urfave/cli/v2"
)

const exitCodeInterrupt = 2

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
		os.Exit(exitCodeInterrupt)
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
				return fmt.Errorf("failed to parse provided url: %w", err)
			}

			if !ctx.Bool("noprompt") {
				prompt := promptui.Prompt{
					Label: "Password",
					Mask:  '*',
				}
				password, err := prompt.Run()
				if err != nil {
					return fmt.Errorf("failed to provide password: %w", err)
				}
				u.User = url.UserPassword(u.User.String(), password)
			}

			conn, err := amqp.DialTLS(u.String(), &tls.Config{InsecureSkipVerify: ctx.Bool("insecure")})
			if err != nil {
				return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
			}
			defer conn.Close()

			ch, err := conn.Channel()
			if err != nil {
				return fmt.Errorf("failed to open a channel: %w", err)
			}
			defer ch.Close()

			err = ch.ExchangeDeclarePassive(ctx.String("exchange"), "topic", false, true, false, false, nil)
			if err != nil {
				return fmt.Errorf("failed to connect to exchange: %w", err)
			}

			q, err := ch.QueueDeclare(fmt.Sprintf("%s.%s", ctx.String("queue"), uuid.NewString()), false, true, false, false, nil)
			if err != nil {
				return fmt.Errorf("failed to declare a queue: %w", err)
			}
			ch.QueueBind(q.Name, ctx.String("bind"), ctx.String("exchange"), false, nil)

			msgs, err := ch.Consume(q.Name, "", true, false, false, false, nil)
			if err != nil {
				return fmt.Errorf("failed to register a consumer: %w", err)
			}

			go func() {
				for d := range msgs {
					log.Printf("📧 Received a message on queue %s: %s", d.RoutingKey, d.Body)
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
