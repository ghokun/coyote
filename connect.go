package main

import (
	"crypto/tls"
	"log"
	"net/url"

	"github.com/ghokun/coyote/auth"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/urfave/cli/v3"
)

func connect(cli *cli.Command) (connection *amqp.Connection, err error) {
	var amqpUrl *url.URL
	if cli.Bool("oauth") {
		log.Printf("🔑 Using OAuth 2.0 authentication")
		amqpUrl, err = auth.OAuth20(cli)
	} else {
		log.Printf("🔑 Using basic authentication")
		amqpUrl, err = auth.Basic(cli)
	}
	if err != nil {
		return nil, err
	}
	return amqp.DialTLS(amqpUrl.String(), &tls.Config{InsecureSkipVerify: cli.Bool("insecure")})
}
