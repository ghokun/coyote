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
	rawURL, insecure, err := ResolveURL(cli)
	if err != nil {
		return nil, err
	}
	return DialURL(rawURL, insecure)
}

// ResolveURL performs CLI-side auth (prompt/browser as needed) and returns
// the fully-resolved AMQP URL including any secret. Must run in the
// foreground client — never in the detached daemon.
func ResolveURL(cli *cli.Command) (rawURL string, insecure bool, err error) {
	var amqpUrl *url.URL
	if cli.Bool("oauth") {
		if !cli.IsSet("redirect-url") {
			return "", false, failed.Because("redirect-url must be set for OAuth 2.0", err)
		}
		log.Printf("🔑 Using OAuth 2.0 authentication")
		amqpUrl, err = auth.OAuth2(cli)
	} else {
		log.Printf("🔑 Using basic authentication")
		amqpUrl, err = auth.Basic(cli)
	}
	if err != nil {
		return "", false, err
	}
	return amqpUrl.String(), cli.Bool("insecure"), nil
}

// DialURL dials an already-resolved AMQP URL. Used by the daemon task runner
// (secret already in hand) and the foreground compat path.
func DialURL(rawURL string, insecure bool) (*amqp.Connection, error) {
	return amqp.DialTLS(rawURL, &tls.Config{InsecureSkipVerify: insecure})
}
