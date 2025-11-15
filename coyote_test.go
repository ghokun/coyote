package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/andreyvit/diff"
	"github.com/cucumber/godog"
)

type ctxKey struct{}

func TestFeatures(t *testing.T) {
	suite := godog.TestSuite{
		TestSuiteInitializer: InitializeTestSuite,
		ScenarioInitializer:  InitializeScenario,
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    []string{"features"},
			TestingT: t,
		},
	}

	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run feature tests")
	}
}

func InitializeTestSuite(ctx *godog.TestSuiteContext) {
	cmd := exec.Command("go", "build", ".")
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}

	ctx.AfterSuite(func() {
		err := os.Remove("coyote")
		if err != nil {
			log.Fatal(err)
		}
	})
}

func InitializeScenario(ctx *godog.ScenarioContext) {
	ctx.Given(`^coyote is present locally$`, coyoteIsPresentLocally)
	ctx.Given(`^brew is present$`, brewIsPresent)

	ctx.When(`^coyote is run with help option$`, coyoteIsRunWithHelpOption)
	ctx.When(`^coyote is run with version option$`, coyoteIsRunWithVersionOption)
	ctx.When(`^"(.+)" is installed using brew$`, formulaIsInstalledUsingBrew)

	ctx.Then(`^help message is printed$`, helpMessageIsPrinted)
	ctx.Then(`^version message is printed$`, versionMessageIsPrinted)
	ctx.Then(`^coyote is installed$`, coyoteIsInstalled)
}

func coyoteIsPresentLocally() error {
	_, err := os.Stat("coyote")
	return err
}

func brewIsPresent() error {
	_, err := exec.LookPath("brew")
	return err
}

func coyoteIsRunWithHelpOption(ctx context.Context) (context.Context, error) {
	out, err := exec.Command("./coyote", "-h").Output()
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, ctxKey{}, string(out)), nil
}

func coyoteIsRunWithVersionOption(ctx context.Context) (context.Context, error) {
	out, err := exec.Command("./coyote", "-v").Output()
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, ctxKey{}, string(out)), nil
}

func formulaIsInstalledUsingBrew(formula string) error {
	cmd := exec.Command("brew", "install", formula)
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func helpMessageIsPrinted(ctx context.Context) error {
	actualOutput := strings.TrimRight(ctx.Value(ctxKey{}).(string), "\n")
	if actualOutput == expectedHelpOutput {
		return nil
	}
	return fmt.Errorf("Result not as expected:\n%v", diff.LineDiff(expectedHelpOutput, actualOutput))
}

func versionMessageIsPrinted(ctx context.Context) error {
	actualOutput := strings.TrimRight(ctx.Value(ctxKey{}).(string), "\n")
	if actualOutput == expectedVersionOutput {
		return nil
	}
	return fmt.Errorf("Result not as expected:\n%v", diff.LineDiff(expectedVersionOutput, actualOutput))
}

func coyoteIsInstalled() error {
	_, err := exec.LookPath("coyote")
	return err
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
   --oauth                                                Use OAuth 2.0 for authentication.
   --redirect-url string                                  OIDC callback url for OAuth 2.0
   --insecure                                             Skips certificate verification.
   --exchange string=string [ --exchange string=string ]  Exchange & routing key combinations to listen messages.
   --queue string                                         Interceptor queue name. If provided, interceptor queue will not be auto deleted.
   --store string                                         SQLite filename to store events.
   --silent                                               Disables terminal print.
   --help, -h                                             show help
   --version, -v                                          print the version`
)
