package e2e

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/andreyvit/diff"
)

func coyoteIsRunWithHelpOption(ctx context.Context) (context.Context, error) {
	out, err := exec.Command("./coyote", "-h").Output()
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, ctxKey{}, string(out)), nil
}

func helpMessageIsPrinted(ctx context.Context) error {
	actualOutput := strings.TrimRight(ctx.Value(ctxKey{}).(string), "\n")
	if actualOutput == expectedHelpOutput {
		return nil
	}
	return fmt.Errorf("Result not as expected:\n%v", diff.LineDiff(expectedHelpOutput, actualOutput))
}

func coyoteIsRunWithVersionOption(ctx context.Context) (context.Context, error) {
	out, err := exec.Command("./coyote", "-v").Output()
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, ctxKey{}, string(out)), nil
}

func versionMessageIsPrinted(ctx context.Context) error {
	actualOutput := strings.TrimRight(ctx.Value(ctxKey{}).(string), "\n")
	if actualOutput == expectedVersionOutput {
		return nil
	}
	return fmt.Errorf("Result not as expected:\n%v", diff.LineDiff(expectedVersionOutput, actualOutput))
}

const (
	expectedVersionOutput = `coyote version development`
	expectedHelpOutput    = `NAME:
   coyote - Coyote is a RabbitMQ message sink.

USAGE:
   coyote [global options]

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
    --exchange myexchange1=#,myexchange2=mykey2      # Messages with or without specific routing keys in multiple exchanges

VERSION:
   development

GLOBAL OPTIONS:
   --url string                                           RabbitMQ url, must start with amqps:// or amqp://.
   --oauth                                                Use OAuth 2.0 for authentication. (default: false)
   --redirect-url string                                  OIDC callback url for OAuth 2.0
   --insecure                                             Skips certificate verification. (default: false)
   --exchange string=string [ --exchange string=string ]  Exchange & routing key combinations to listen messages.
   --queue string                                         Interceptor queue name. If provided, interceptor queue will not be auto deleted.
   --store string                                         SQLite filename to store events.
   --silent                                               Disables terminal print. (default: false)
   --help, -h                                             show help
   --version, -v                                          print the version`
)
