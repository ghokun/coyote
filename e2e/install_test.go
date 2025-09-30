package e2e

import "os/exec"

func brewIsPresent() error {
	_, err := exec.LookPath("brew")
	return err
}

func formulaIsInstalledUsingBrew(formula string) error {
	cmd := exec.Command("brew", "install", formula)
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

func coyoteIsInstalled() error {
	_, err := exec.LookPath("coyote")
	return err
}
