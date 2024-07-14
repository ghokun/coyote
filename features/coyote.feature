Feature: run coyote

  Scenario: Smoke test
    Given coyote is present
    When coyote is run with help option
    Then help message is printed
