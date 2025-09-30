package auth

import (
	"github.com/cqroot/prompt"
	"github.com/cqroot/prompt/input"
	failed "github.com/ghokun/coyote/error"
	"github.com/urfave/cli/v3"
	"net/url"
)

func Basic(cli *cli.Command) (amqpUrl *url.URL, err error) {
	amqpUrl, err = url.Parse(cli.String("url"))
	if err != nil {
		return nil, failed.Because("failed to parse provided url", err)
	}
	username, err := retrieveUsername(amqpUrl)
	if err != nil {
		return nil, failed.Because("failed to provide username", err)
	}
	password, err := retrievePassword(amqpUrl)
	if err != nil {
		return nil, failed.Because("failed to provide password:", err)
	}
	amqpUrl.User = url.UserPassword(username, password)
	return amqpUrl, nil
}

func retrieveUsername(amqpUrl *url.URL) (username string, err error) {
	if amqpUrl.User != nil && amqpUrl.User.Username() != "" {
		return amqpUrl.User.Username(), nil
	}
	return prompt.
		New().
		Ask("Username").
		Input("", input.WithCharLimit(0), input.WithEchoMode(input.EchoNormal))
}

func retrievePassword(amqpUrl *url.URL) (username string, err error) {
	if amqpUrl.User != nil {
		if password, passwordSet := amqpUrl.User.Password(); passwordSet && password != "" {
			return password, nil
		}
	}
	return prompt.
		New().
		Ask("Password").
		Input("", input.WithCharLimit(0), input.WithEchoMode(input.EchoPassword))
}
