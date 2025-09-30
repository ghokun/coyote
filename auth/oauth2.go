package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cqroot/prompt"
	"github.com/cqroot/prompt/choose"
	"github.com/fatih/color"
	failed "github.com/ghokun/coyote/error"
	"github.com/hashicorp/go-secure-stdlib/base62"
	"github.com/pkg/browser"
	"github.com/urfave/cli/v3"
	"golang.org/x/oauth2"
)

type OAuthConfig struct {
	OAuthEnabled          bool                            `json:"oauth_enabled"`
	OAuthResourceServers  map[string]*OAuthResourceServer `json:"oauth_resource_servers"`
	OAuthDisableBasicAuth bool                            `json:"oauth_disable_basic_auth"`
	OAuthClientID         string                          `json:"oauth_client_id"`
	OAuthScopes           string                          `json:"oauth_scopes"`
}

type OAuthResourceServer struct {
	ID               string `json:"id"`
	OAuthProviderURL string `json:"oauth_provider_url"`
}

type OpenidConfiguration struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
}

func OAuth2(cli *cli.Command) (amqpUrl *url.URL, err error) {
	amqpUrl, err = url.Parse(cli.String("url"))
	if err != nil {
		return nil, failed.Because("failed to parse provided url", err)
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

	openIdConfiguration, err := fetchOpenidConfiguration(choice.OAuthProviderURL)
	if err != nil {
		return nil, err
	}

	// Build authorization code URL
	redirectUrl := cli.String("redirect-url")
	conf := &oauth2.Config{
		ClientID:    oauthConfig.OAuthClientID,
		RedirectURL: redirectUrl,
		Scopes:      strings.Split(oauthConfig.OAuthScopes, " "),
		Endpoint: oauth2.Endpoint{
			AuthURL:  openIdConfiguration.AuthorizationEndpoint,
			TokenURL: openIdConfiguration.TokenEndpoint,
		},
	}
	audiance := oauth2.SetAuthURLParam("audience", choice.ID)
	resource := oauth2.SetAuthURLParam("resource", choice.ID)
	responseMode := oauth2.SetAuthURLParam("response_mode", "query")
	state := base62.MustRandom(32)
	verifier := oauth2.GenerateVerifier()
	consentPage := conf.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), audiance, resource, responseMode)

	// Run web server
	token, err := serveForCallback(conf, redirectUrl, state, verifier, consentPage)
	if err != nil {
		return nil, err
	}

	// Set user name and password in the amqp url
	amqpUrl.User = url.UserPassword(oauthConfig.OAuthClientID, token.AccessToken)
	return amqpUrl, nil
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
		return nil, failed.Because("failure while connecting to "+authApiUrl, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Fatal(failed.Because("failed to close auth config response body", err))
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, failed.Because("failed to fetch auth config, status code: "+resp.Status, nil)
	}
	err = json.NewDecoder(resp.Body).Decode(&authConfig)
	if err != nil && err != io.EOF {
		return nil, failed.Because("failed to decode auth config", err)
	}
	if authConfig == nil {
		return nil, failed.Because("received empty auth config", nil)
	}
	if !authConfig.OAuthEnabled {
		return nil, failed.Because("OAuth 2.0 is not enabled on the server", nil)
	}
	return authConfig, nil
}

func promptAuthServer(oauthConfig *OAuthConfig) (choice *OAuthResourceServer, err error) {
	var choices []choose.Choice
	for id, server := range oauthConfig.OAuthResourceServers {
		choices = append(choices, choose.Choice{Text: id, Note: server.OAuthProviderURL})
	}
	choices = append(choices, choose.Choice{Text: "none", Note: "Quits the program"})
	id, err := prompt.
		New().
		Ask("Choose an OAuth 2.0 resource server:").
		AdvancedChoose(choices)

	if id == "none" {
		return nil, failed.Because("no resource server chosen", nil)
	}
	return oauthConfig.OAuthResourceServers[id], err
}

func fetchOpenidConfiguration(oauthProviderUrl string) (config *OpenidConfiguration, err error) {
	wellKnownUrl := oauthProviderUrl + "/.well-known/openid-configuration"
	resp, err := http.Get(wellKnownUrl)
	if err != nil {
		return nil, failed.Because("failure while connecting to "+wellKnownUrl, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Fatal(failed.Because("failed to close openid configuration response body", err))
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, failed.Because("failed to fetch openid configuration, status code: "+resp.Status, nil)
	}
	err = json.NewDecoder(resp.Body).Decode(&config)
	if err != nil && err != io.EOF {
		return nil, failed.Because("failed to decode openid configuration", err)
	}
	if config == nil {
		return nil, failed.Because("received empty openid configuration", nil)
	}
	return config, nil
}

func serveForCallback(conf *oauth2.Config, redirectUrl string, state string, verifier string, consentPage string) (token *oauth2.Token, err error) {
	parsedUrl, err := url.Parse(redirectUrl)
	if err != nil {
		return nil, failed.Because("failed to parse redirect url", err)
	}

	var ch = make(chan bool, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(parsedUrl.Path, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "State parameter doesn't match", http.StatusBadRequest)
			return
		}
		if errMsg := r.URL.Query().Get("error"); errMsg != "" {
			desc := r.URL.Query().Get("error_description")
			http.Error(w, "Authorization server returned error: "+errMsg+" - "+desc, http.StatusBadRequest)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Code parameter missing in callback", http.StatusBadRequest)
			return
		}

		token, err = conf.Exchange(context.Background(), code, oauth2.VerifierOption(verifier))
		if err != nil {
			http.Error(w, "Failed to exchange code for token: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Display success message
		w.Header().Set("Content-Type", "text/html")
		_, err = fmt.Fprint(w, successHtml)
		if err != nil {
			log.Println("⚠️ Failed to generate success page:", err)
		}

		// Notify main goroutine
		ch <- true
	})

	server := &http.Server{
		Addr:    ":" + parsedUrl.Port(),
		Handler: mux,
	}

	go func() {
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()
	defer func() {
		if err := server.Shutdown(context.Background()); err != nil {
			log.Fatal(failed.Because("failed to shutdown callback server", err))
		}
	}()

	log.Println("🌐 Opening browser for authentication, if browser does not open automatically, please navigate to following URL manually\n\n" + color.YellowString(consentPage) + "\n")
	err = browser.OpenURL(consentPage)
	if err != nil {
		log.Println("⚠️ Failed to open browser automatically.", err)
	}

	select {
	case <-time.After(1 * time.Minute):
		err = failed.Because("timeout waiting for OAuth 2.0 callback", nil)
	case <-ch:
		log.Println("✅ Authentication successful!")
	}
	return token, nil
}

const successHtml = `
<html>
<head>
	<title>Authentication Successful</title>
	<style>
		body { font-family: Arial, sans-serif; text-align: center; padding: 50px; }
		h1 { color: #4CAF50; }
		p { font-size: 18px; }
	</style>
	</head>
<body>
	<h1>Authentication Successful</h1>
	<p>You can close this window and return to the application.</p>
</body>
</html>
`
