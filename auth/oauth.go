package auth

import (
	. "github.com/ghokun/coyote/error"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/url"

	"github.com/cqroot/prompt"
	"github.com/cqroot/prompt/choose"
	"github.com/fatih/color"
	"github.com/urfave/cli/v3"
)

type OAuthConfig struct {
	OauthEnabled          bool                            `json:"oauth_enabled"`
	OauthResourceServers  map[string]*OauthResourceServer `json:"oauth_resource_servers"`
	OauthDisableBasicAuth bool                            `json:"oauth_disable_basic_auth"`
	OauthClientID         string                          `json:"oauth_client_id"`
	OauthScopes           string                          `json:"oauth_scopes"`
}

type OauthResourceServer struct {
	ID               string `json:"id"`
	OauthProviderURL string `json:"oauth_provider_url"`
}

func OAuth20(cli *cli.Command) (amqpUrl *url.URL, err error) {
	amqpUrl, err = url.Parse(cli.String("url"))
	if err != nil {
		return nil, Because("failed to parse provided url", err)
	}
	oauthConfig, err := fetchAuthConfig(amqpUrl)
	if err != nil {
		return nil, err
	}
	choice, err := promptAuthServer(oauthConfig)
	if err != nil {
		return nil, err
	}
	log.Printf("🔑 Chosen resource server: %s", color.YellowString(choice.ID))

	return nil, nil
}

func fetchAuthConfig(amqpUrl *url.URL) (authConfig *OAuthConfig, err error) {
	var apiScheme string
	if amqpUrl.Scheme == "amqps" {
		apiScheme = "https"
	} else {
		apiScheme = "http"
	}
	authApiUrl := apiScheme + "://" + amqpUrl.Host + "/api/auth"
	resp, err := http.Get(authApiUrl)
	if err != nil {
		return nil, Because("failure while connecting to "+authApiUrl, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, Because("failed to fetch auth config, status code: "+resp.Status, nil)
	}
	err = json.NewDecoder(resp.Body).Decode(&authConfig)
	if err != nil && err != io.EOF {
		return nil, Because("failed to decode auth config", err)
	}
	if authConfig == nil {
		return nil, Because("received empty auth config", nil)
	}
	if !authConfig.OauthEnabled {
		return nil, Because("OAuth 2.0 is not enabled on the server", nil)
	}
	return authConfig, nil
}

func promptAuthServer(oauthConfig *OAuthConfig) (choice *OauthResourceServer, err error) {
	var choices []choose.Choice
	for id, server := range oauthConfig.OauthResourceServers {
		choices = append(choices, choose.Choice{Text: id, Note: server.OauthProviderURL})
	}
	choices = append(choices, choose.Choice{Text: "none", Note: "Quits the program"})
	id, err := prompt.
		New().
		Ask("Choose an OAuth 2.0 resource server:").
		AdvancedChoose(choices)

	if id == "none" {
		return nil, Because("no resource server chosen", nil)
	}
	return oauthConfig.OauthResourceServers[id], err
}
