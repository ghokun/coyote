package e2e

import (
	"log"
	"os"
	"os/exec"
	"testing"

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
	cmd := exec.Command("go", "build", "-cover", "./..")
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
