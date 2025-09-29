package main

import (
	"fmt"
	"github.com/fatih/color"
)

func because(reason string, err error) error {
	if err == nil {
		return fmt.Errorf("💥 %s", color.RedString(reason))
	}
	return fmt.Errorf("💥 %s: %w", color.RedString(reason), err)
}
