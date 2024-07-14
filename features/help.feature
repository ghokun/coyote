Feature: Run coyote

  Scenario: Coyote displays help message
    Given coyote is present locally
    When coyote is run with help option
    Then help message is printed
