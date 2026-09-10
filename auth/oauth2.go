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
	"sync"
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

	ch := make(chan callbackResult, 1)
	var deliverOnce sync.Once
	deliver := func(res callbackResult) {
		// Only the first successful exchange is delivered. Duplicate
		// callbacks (browser retry/prefetch) are answered with the success
		// page again but must neither block on a full channel (goroutine
		// leak) nor overwrite the first result (data race).
		deliverOnce.Do(func() {
			ch <- res
		})
	}
	mux := http.NewServeMux()
	mux.HandleFunc(parsedUrl.Path, newCallbackHandler(state, func(code string) (*oauth2.Token, error) {
		return conf.Exchange(context.Background(), code, oauth2.VerifierOption(verifier))
	}, deliver))

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
	if err := browser.OpenURL(consentPage); err != nil {
		log.Println("⚠️ Failed to open browser automatically.", err)
	}

	select {
	case <-time.After(1 * time.Minute):
		return nil, failed.Because("timeout waiting for OAuth 2.0 callback", nil)
	case res := <-ch:
		log.Println("✅ Authentication successful!")
		return res.token, nil
	}
}

// callbackResult carries the outcome of the OAuth callback exchange from the
// HTTP handler goroutine back to the main flow. Sending the result over the
// channel (instead of writing shared outer variables from the handler) gives
// a single synchronization point with no shared mutable state.
type callbackResult struct {
	token *oauth2.Token
}

// newCallbackHandler builds the OAuth callback HTTP handler. Validation
// failures and token-exchange failures are reported via HTTP errors without
// delivering a result, so the user can still retry within the auth window.
// The first successful exchange is passed to deliver exactly once by the
// caller; duplicate callbacks get the success page again but their results
// are dropped.
func newCallbackHandler(state string, exchange func(code string) (*oauth2.Token, error), deliver func(callbackResult)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		token, err := exchange(code)
		if err != nil {
			http.Error(w, "Failed to exchange code for token: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Display success message
		w.Header().Set("Content-Type", "text/html")
		if _, err := fmt.Fprint(w, successHtml); err != nil {
			log.Println("⚠️ Failed to generate success page:", err)
		}

		// Notify main goroutine
		deliver(callbackResult{token: token})
	}
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
