package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"

	"crypto/x509"

	"github.com/fatih/color"
	"github.com/google/uuid"
	"github.com/manifoldco/promptui"
	amqp "github.com/rabbitmq/amqp091-go"
	cli "github.com/urfave/cli/v2"
)

const (
	exitCodeInterrupt = 2
	urlFlag           = "url"
	exchangeFlag      = "exchange"
	typeFlag          = "type"
	bindFlag          = "bind"
	queueFlag         = "queue"
	insecureFlag      = "insecure"
	nopromptFlag      = "noprompt"
	nopassiveFlag     = "nopassive"
	rootCertFlag      = "rootca"
	certFlag          = "cert"
	keyFlag           = "key"
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
		os.Exit(exitCodeInterrupt)
	}()

	app := &cli.App{
		Name:    "coyote",
		Usage:   "Coyote is a RabbitMQ message sink.",
		Version: Version,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     urlFlag,
				Required: true,
				Usage:    "RabbitMQ url, must start with amqps:// or amqp://.",
			},
			&cli.StringFlag{
				Name:     exchangeFlag,
				Required: true,
				Usage:    "Exchange name to listen messages.",
			},
			&cli.StringFlag{
				Name:  typeFlag,
				Value: "topic",
				Usage: "Exchange type. Valid values are direct, fanout, topic and headers.",
			},
			&cli.StringFlag{
				Name:  bindFlag,
				Value: "#",
				Usage: "Routing key to bind. Binds to all queues if not provided.",
			},
			&cli.StringFlag{
				Name:  queueFlag,
				Value: "interceptor",
				Usage: "Interceptor queue name.",
			},
			&cli.BoolFlag{
				Name:  insecureFlag,
				Usage: "Skips certificate verification.",
			},
			&cli.BoolFlag{
				Name:  nopromptFlag,
				Usage: "Disables password prompt.",
			},
			&cli.BoolFlag{
				Name:  nopassiveFlag,
				Usage: "Declares queue actively.",
			},
			&cli.PathFlag{
				Name:  rootCertFlag,
				Usage: "Root certificate path",
			},
			&cli.PathFlag{
				Name:  certFlag,
				Usage: "Client certificate path",
			},
			&cli.PathFlag{
				Name:  keyFlag,
				Usage: "Client certificate key path",
			},
		},
		Action: func(ctx *cli.Context) error {
			u, err := validateInput(ctx)
			if err != nil {
				return err
			}
			msgs, err := consumeMessages(ctx, u.String())
			if err != nil {
				return sprintErr("failed to register a consumer:", err)
			}
			go tailMessages(msgs)
			log.Printf("⏳ Waiting for messages. To exit press CTRL+C")
			<-ctx.Done()
			return nil
		},
	}

	if err := app.RunContext(ctx, os.Args); err != nil {
		log.Fatal(err)
	}
}

func validateInput(ctx *cli.Context) (u *url.URL, err error) {
	u, err = url.Parse(ctx.String(urlFlag))
	if err != nil {
		return nil, sprintErr("failed to parse provided url:", err)
	}
	if !ctx.Bool(nopromptFlag) {
		prompt := promptui.Prompt{
			Label: "Password",
			Mask:  '*',
		}
		password, err := prompt.Run()
		if err != nil {
			return nil, sprintErr("failed to provide password:", err)
		}
		u.User = url.UserPassword(u.User.String(), password)
	}
	return u, nil
}

func tlsClientConfig(ctx *cli.Context) (cfg *tls.Config, err error) {
	cfg = new(tls.Config)
	cfg.InsecureSkipVerify = ctx.Bool(insecureFlag)
	if cfg.InsecureSkipVerify {
		return cfg, nil
	}
	if ctx.Path(rootCertFlag) != "" {
		cfg.RootCAs = x509.NewCertPool()
		caCert, err := os.ReadFile(ctx.Path(rootCertFlag))
		if err != nil {
			return nil, sprintErr("failed to read root certiticate", err)
		}
		cfg.RootCAs.AppendCertsFromPEM(caCert)
	}
	if ctx.Path(certFlag) != "" && ctx.Path(keyFlag) != "" {
		cert, err := os.ReadFile(ctx.Path(certFlag))
		if err != nil {
			return nil, sprintErr("failed to read certiticate", err)
		}
		key, err := os.ReadFile(ctx.Path(keyFlag))
		if err != nil {
			return nil, sprintErr("failed to read certiticate key", err)
		}
		certPair, err := tls.X509KeyPair(cert, key)
		if err != nil {
			return nil, sprintErr("client config error", err)
		}
		cfg.Certificates = append(cfg.Certificates, certPair)
	}
	return cfg, nil
}

func consumeMessages(ctx *cli.Context, url string) (msgs <-chan amqp.Delivery, err error) {
	tlsConfig, err := tlsClientConfig(ctx)
	if err != nil {
		return nil, err
	}
	conn, err := amqp.DialTLS(url, tlsConfig)
	if err != nil {
		return nil, sprintErr("failed to connect to RabbitMQ:", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return nil, sprintErr("failed to open a channel:", err)
	}
	defer ch.Close()

	exchangeType := ctx.String(typeFlag)
	if ctx.Bool(nopassiveFlag) {
		err = ch.ExchangeDeclare(
			ctx.String(exchangeFlag), // exchange name
			exchangeType,             // exchange type
			false,                    // durable
			true,                     // auto delete
			false,                    // internal
			false,                    // no wait
			nil,                      // table arguments
		)
	} else {
		err = ch.ExchangeDeclarePassive(
			ctx.String(exchangeFlag), // exchange name
			exchangeType,             // exchange type
			false,                    // durable
			true,                     // auto delete
			false,                    // internal
			false,                    // no wait
			nil,                      // table arguments
		)
	}
	if err != nil {
		return nil, sprintErr("failed to connect to exchange:", err)
	}
	q, err := ch.QueueDeclare(
		fmt.Sprintf("%s.%s", ctx.String(queueFlag), uuid.NewString()), // queue name
		false, // durable
		true,  // auto delete
		false, // exclusive
		false, // no wait
		nil,   // table arguments
	)
	if err != nil {
		return nil, sprintErr("failed to declare a queue:", err)
	}
	ch.QueueBind(
		q.Name,                   // queue name
		ctx.String(bindFlag),     // routing key
		ctx.String(exchangeFlag), // exchange name
		false,                    // no wait
		nil,                      // table arguments
	)
	if err != nil {
		return nil, sprintErr("failed to bind the queue:", err)
	}
	return ch.Consume(
		q.Name, // queue name
		"",     // consumer
		true,   // auto ack
		false,  // exclusive
		false,  // no local
		false,  // no wait
		nil,    // table arguments
	)
}

func tailMessages(msgs <-chan amqp.Delivery) {
	for d := range msgs {
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
	}
}

func sprintErr(msg string, err error) error {
	return fmt.Errorf("%s %w", color.RedString(msg), err)
}
