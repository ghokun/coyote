Feature: Install coyote

  Scenario: Homebrew installs coyote
    Given brew is present
    When "ghokun/tap/coyote" is installed using brew
    Then coyote is installed
