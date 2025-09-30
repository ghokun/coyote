package main

import (
	"crypto/tls"
	"log"
	"net/url"

	"github.com/ghokun/coyote/auth"
	failed "github.com/ghokun/coyote/error"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/urfave/cli/v3"
)

func connect(cli *cli.Command) (connection *amqp.Connection, err error) {
	var amqpUrl *url.URL
	if cli.Bool("oauth") {
		if !cli.IsSet("redirect-url") {
			return nil, failed.Because("redirect-url must be set for OAuth 2.0", err)
		}
		log.Printf("🔑 Using OAuth 2.0 authentication")
		amqpUrl, err = auth.OAuth2(cli)
	} else {
		log.Printf("🔑 Using basic authentication")
		amqpUrl, err = auth.Basic(cli)
	}
	if err != nil {
		return nil, err
	}
	return amqp.DialTLS(amqpUrl.String(), &tls.Config{InsecureSkipVerify: cli.Bool("insecure")})
}
