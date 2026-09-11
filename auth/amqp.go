package auth

import (
	"fmt"
	"net/url"

	failed "github.com/ghokun/coyote/error"
)

// ValidateAMQPScheme ensures the URL uses amqp:// or amqps:// as promised
// by the --url flag help text. It must be called right after parsing the
// URL and before the URL is used to dial or to derive other URLs, so a
// typo'd scheme fails fast instead of falling through to insecure defaults.
func ValidateAMQPScheme(amqpUrl *url.URL) error {
	if amqpUrl == nil {
		return failed.Because("url must start with amqp:// or amqps://", nil)
	}
	switch amqpUrl.Scheme {
	case "amqp", "amqps":
		return nil
	default:
		return failed.Because(fmt.Sprintf("url scheme must be amqp:// or amqps://, got %q", amqpUrl.Scheme), nil)
	}
}
