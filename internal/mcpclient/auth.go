package mcpclient

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Authorization, as the MCP spec has it (2025-06-18):
//
//	1. The server answers 401 and names its protected-resource metadata in
//	   WWW-Authenticate (RFC 9728); without the header, the well-known paths.
//	2. That metadata names the authorization server, whose own metadata
//	   (RFC 8414, or OpenID discovery) gives the endpoints.
//	3. The client registers itself (RFC 7591) — there is no client id to paste.
//	4. Authorization code with PKCE S256 through the system browser, back to a
//	   loopback address this process listens on (RFC 8252), with the server
//	   named as the token's resource (RFC 8707) so it is minted for it alone.
//	5. Refresh when the access token runs out.
//
// What comes back is an Authorization the caller stores whole: the endpoints
// and the registration are needed again to refresh, and discovering them on
// every refresh would be two extra round trips for an answer that does not
// change.
// ---------------------------------------------------------------------------

// ErrGrantRevoked is a refresh the authorization server refused: the producer
// revoked the grant, or it expired. Only a new sign-in gets past it.
var ErrGrantRevoked = errors.New("the server no longer accepts this sign-in — authorize it again")

// ErrNoAuthorizationNeeded is a server that answered without asking for a
// token: there is nothing to sign in to, and it can be used as it is.
var ErrNoAuthorizationNeeded = errors.New("this server does not ask for authorization")

// ResourceMetadata is RFC 9728's protected-resource metadata.
type ResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported,omitempty"`
}

// ServerMetadata is the part of RFC 8414's metadata a client uses.
type ServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint,omitempty"`
	RevocationEndpoint                string   `json:"revocation_endpoint,omitempty"`
	ScopesSupported                   []string `json:"scopes_supported,omitempty"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported,omitempty"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported,omitempty"`
}

// Registration is what dynamic client registration handed back.
type Registration struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret,omitempty"`
	// RedirectURI is the loopback address registered. Kept because a server
	// that matches redirect URIs exactly refuses any other port, so the next
	// sign-in listens on this one when it can and registers again when not.
	RedirectURI string `json:"redirectUri"`
	// AuthMethod is how the token endpoint is to be told who is asking.
	AuthMethod string `json:"authMethod,omitempty"`
}

// Token is an access token and what renews it.
type Token struct {
	Access  string    `json:"access"`
	Refresh string    `json:"refresh,omitempty"`
	Expiry  time.Time `json:"expiry"`
	Scope   string    `json:"scope,omitempty"`
}

// Valid reports whether the access token has at least a minute left. A token
// with no expiry is taken at its word until the server refuses it.
func (t Token) Valid() bool {
	if t.Access == "" {
		return false
	}
	return t.Expiry.IsZero() || time.Until(t.Expiry) > time.Minute
}

// Authorization is everything a signed-in connection keeps.
type Authorization struct {
	Resource string         `json:"resource"`
	Scopes   []string       `json:"scopes,omitempty"`
	Server   ServerMetadata `json:"server"`
	Client   Registration   `json:"client"`
	Token    Token          `json:"token"`
}

// Challenge is what a 401 said about how to authorize.
type Challenge struct {
	// Required is false when the server answered without asking for a token.
	Required         bool
	ResourceMetadata string
	Scope            string
}

// Probe asks the server, with no token, whether it wants one. An initialize
// rather than a GET: a Streamable HTTP server need not answer GET at all, and
// a POST is what every real call will be.
func Probe(ctx context.Context, httpc *http.Client, serverURL string) (Challenge, error) {
	if httpc == nil {
		httpc = defaultHTTP
	}
	body := `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"` +
		ProtocolVersion + `","capabilities":{},"clientInfo":{"name":"probe","version":"0"}}}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL, strings.NewReader(body))
	if err != nil {
		return Challenge{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := httpc.Do(req)
	if err != nil {
		return Challenge{}, fmt.Errorf("could not reach %s: %w", serverURL, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusUnauthorized {
		return Challenge{}, nil
	}
	params := bearerParams(resp.Header.Values("WWW-Authenticate"))
	return Challenge{Required: true, ResourceMetadata: params["resource_metadata"], Scope: params["scope"]}, nil
}

// bearerParams reads the auth-params of a Bearer challenge.
func bearerParams(headers []string) map[string]string {
	out := map[string]string{}
	for _, h := range headers {
		rest := strings.TrimSpace(h)
		if len(rest) < 6 || !strings.EqualFold(rest[:6], "bearer") {
			continue
		}
		rest = rest[6:]
		for rest != "" {
			rest = strings.TrimLeft(rest, " ,")
			eq := strings.IndexByte(rest, '=')
			if eq < 0 {
				break
			}
			key := strings.ToLower(strings.TrimSpace(rest[:eq]))
			rest = rest[eq+1:]
			var value string
			if strings.HasPrefix(rest, `"`) {
				end := 1
				for end < len(rest) && rest[end] != '"' {
					if rest[end] == '\\' {
						end++
					}
					end++
				}
				value = strings.ReplaceAll(rest[1:min(end, len(rest))], `\"`, `"`)
				rest = rest[min(end+1, len(rest)):]
			} else {
				comma := strings.IndexByte(rest, ',')
				if comma < 0 {
					comma = len(rest)
				}
				value = strings.TrimSpace(rest[:comma])
				rest = rest[comma:]
			}
			out[key] = value
		}
	}
	return out
}

// Discover finds the server's protected-resource metadata and its
// authorization server's metadata.
func Discover(ctx context.Context, httpc *http.Client, serverURL string, challenge Challenge) (ResourceMetadata, ServerMetadata, error) {
	if httpc == nil {
		httpc = defaultHTTP
	}
	server, err := url.Parse(serverURL)
	if err != nil {
		return ResourceMetadata{}, ServerMetadata{}, err
	}
	origin := server.Scheme + "://" + server.Host
	path := strings.TrimSuffix(server.Path, "/")

	candidates := []string{}
	if challenge.ResourceMetadata != "" {
		candidates = append(candidates, challenge.ResourceMetadata)
	}
	if path != "" {
		candidates = append(candidates, origin+"/.well-known/oauth-protected-resource"+path)
	}
	candidates = append(candidates, origin+"/.well-known/oauth-protected-resource")

	var resource ResourceMetadata
	found := false
	for _, address := range candidates {
		if err := getMetadata(ctx, httpc, address, &resource); err == nil && len(resource.AuthorizationServers) > 0 {
			found = true
			break
		}
	}
	if !found {
		// The 2025-03-26 revision had no resource metadata: the server's own
		// origin was its authorization server. Some servers still work that way.
		resource = ResourceMetadata{Resource: serverURL, AuthorizationServers: []string{origin}}
	}
	if resource.Resource == "" {
		resource.Resource = serverURL
	}

	issuer := strings.TrimSuffix(resource.AuthorizationServers[0], "/")
	meta, err := serverMetadata(ctx, httpc, issuer)
	if err != nil {
		return ResourceMetadata{}, ServerMetadata{}, err
	}
	if meta.AuthorizationEndpoint == "" || meta.TokenEndpoint == "" {
		return ResourceMetadata{}, ServerMetadata{}, fmt.Errorf("%s does not say where to sign in", issuer)
	}
	if len(meta.CodeChallengeMethodsSupported) > 0 && !contains(meta.CodeChallengeMethodsSupported, "S256") {
		// The spec makes PKCE mandatory; a server that cannot do S256 is one
		// whose codes are worth stealing.
		return ResourceMetadata{}, ServerMetadata{}, fmt.Errorf("%s does not support PKCE (S256)", issuer)
	}
	return resource, meta, nil
}

// serverMetadata tries RFC 8414's path-inserted address, then OpenID
// discovery both ways, as the MCP spec lists them.
func serverMetadata(ctx context.Context, httpc *http.Client, issuer string) (ServerMetadata, error) {
	u, err := url.Parse(issuer)
	if err != nil {
		return ServerMetadata{}, err
	}
	origin := u.Scheme + "://" + u.Host
	path := strings.TrimSuffix(u.Path, "/")
	var candidates []string
	if path != "" {
		candidates = []string{
			origin + "/.well-known/oauth-authorization-server" + path,
			origin + "/.well-known/openid-configuration" + path,
			origin + path + "/.well-known/openid-configuration",
			origin + path + "/.well-known/oauth-authorization-server",
		}
	} else {
		candidates = []string{
			origin + "/.well-known/oauth-authorization-server",
			origin + "/.well-known/openid-configuration",
		}
	}
	var lastErr error
	for _, address := range candidates {
		var meta ServerMetadata
		if err := getMetadata(ctx, httpc, address, &meta); err != nil {
			lastErr = err
			continue
		}
		if meta.TokenEndpoint != "" {
			return meta, nil
		}
	}
	return ServerMetadata{}, fmt.Errorf("could not read %s's sign-in metadata: %v", issuer, lastErr)
}

func getMetadata(ctx context.Context, httpc *http.Client, address string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %d", address, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

// Register makes this app a client of the authorization server.
func Register(ctx context.Context, httpc *http.Client, meta ServerMetadata, clientName, redirectURI string, scopes []string) (Registration, error) {
	if httpc == nil {
		httpc = defaultHTTP
	}
	if meta.RegistrationEndpoint == "" {
		return Registration{}, errors.New("this server does not let apps register themselves, so it cannot be signed in to from here")
	}
	// A public client: an app shipped as a binary cannot keep a secret, and
	// PKCE is what stands in for one.
	method := "none"
	if len(meta.TokenEndpointAuthMethodsSupported) > 0 && !contains(meta.TokenEndpointAuthMethodsSupported, "none") {
		method = "client_secret_post"
	}
	request := map[string]any{
		"client_name":                clientName,
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": method,
		"application_type":           "native",
	}
	if len(scopes) > 0 {
		request["scope"] = strings.Join(scopes, " ")
	}
	body, _ := json.Marshal(request)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, meta.RegistrationEndpoint, strings.NewReader(string(body)))
	if err != nil {
		return Registration{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := httpc.Do(req)
	if err != nil {
		return Registration{}, fmt.Errorf("could not register with the server: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return Registration{}, fmt.Errorf("the server refused to register this app: %s", oauthMessage(raw, resp.Status))
	}
	var answer struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		AuthMethod   string `json:"token_endpoint_auth_method"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil || answer.ClientID == "" {
		return Registration{}, errors.New("the server's registration answer had no client id")
	}
	if answer.AuthMethod == "" {
		answer.AuthMethod = method
	}
	return Registration{
		ClientID: answer.ClientID, ClientSecret: answer.ClientSecret,
		RedirectURI: redirectURI, AuthMethod: answer.AuthMethod,
	}, nil
}

// AuthorizeOptions is one sign-in.
type AuthorizeOptions struct {
	ServerURL  string
	ClientName string
	// Open shows the sign-in page in the producer's browser.
	Open func(string)
	// Previous is the last authorization, whose registration is reused when
	// its redirect port can be listened on again.
	Previous *Authorization
	HTTP     *http.Client
	// Timeout is how long the browser is waited on; zero is five minutes.
	Timeout time.Duration
	// Listen is how the loopback listener is made — a variable so a test can
	// see which address was asked for.
	Listen func(address string) (net.Listener, error)
}

// SignInTimeout is how long Authorize waits for the browser by default.
const SignInTimeout = 5 * time.Minute

// Authorize runs the whole sign-in and answers the authorization to store.
func Authorize(ctx context.Context, opts AuthorizeOptions) (Authorization, error) {
	httpc := opts.HTTP
	if httpc == nil {
		httpc = defaultHTTP
	}
	listen := opts.Listen
	if listen == nil {
		listen = func(address string) (net.Listener, error) { return net.Listen("tcp", address) }
	}
	challenge, err := Probe(ctx, httpc, opts.ServerURL)
	if err != nil {
		return Authorization{}, err
	}
	if !challenge.Required {
		return Authorization{}, ErrNoAuthorizationNeeded
	}
	resource, meta, err := Discover(ctx, httpc, opts.ServerURL, challenge)
	if err != nil {
		return Authorization{}, err
	}
	scopes := resource.ScopesSupported
	if challenge.Scope != "" {
		scopes = strings.Fields(challenge.Scope)
	}

	// The previous registration's port first, so a server that matches
	// redirect URIs exactly takes the sign-in without a second registration.
	var listener net.Listener
	var client Registration
	if prev := opts.Previous; prev != nil && prev.Client.ClientID != "" &&
		prev.Server.Issuer == meta.Issuer && prev.Client.RedirectURI != "" {
		if u, err := url.Parse(prev.Client.RedirectURI); err == nil {
			if l, err := listen(u.Host); err == nil {
				listener, client = l, prev.Client
			}
		}
	}
	if listener == nil {
		listener, err = listen("127.0.0.1:0")
		if err != nil {
			return Authorization{}, fmt.Errorf("could not listen for the sign-in: %w", err)
		}
		redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", listener.Addr().(*net.TCPAddr).Port)
		client, err = Register(ctx, httpc, meta, opts.ClientName, redirect, scopes)
		if err != nil {
			listener.Close()
			return Authorization{}, err
		}
	}
	defer listener.Close()
	callbackPath := "/callback"
	if u, err := url.Parse(client.RedirectURI); err == nil && u.Path != "" {
		callbackPath = u.Path
	}

	verifier := randomString(64)
	sum := sha256.Sum256([]byte(verifier))
	state := randomString(32)
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {client.ClientID},
		"redirect_uri":          {client.RedirectURI},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"resource":              {resource.Resource},
	}
	if len(scopes) > 0 {
		q.Set("scope", strings.Join(scopes, " "))
	}

	type result struct {
		code string
		err  error
	}
	results := make(chan result, 1)
	server := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only the callback counts; a browser asking for /favicon.ico is not
			// a sign-in that failed.
			if r.URL.Path != callbackPath {
				http.NotFound(w, r)
				return
			}
			got := r.URL.Query()
			var res result
			switch {
			case got.Get("state") != state:
				res.err = errors.New("the sign-in came back for a different request — try again")
			case got.Get("error") != "":
				msg := got.Get("error_description")
				if msg == "" {
					msg = got.Get("error")
				}
				res.err = fmt.Errorf("the sign-in was refused: %s", msg)
			case got.Get("code") == "":
				res.err = errors.New("the sign-in came back without a code")
			default:
				res.code = got.Get("code")
			}
			callbackPage(w, opts.ClientName, res.err)
			select {
			case results <- res:
			default:
			}
		}),
	}
	go func() { _ = server.Serve(listener) }()
	// Shutdown rather than Close: the callback page is still on its way to the
	// browser when the code arrives, and closing cuts it off mid-answer.
	defer func() {
		done, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(done)
	}()

	link := meta.AuthorizationEndpoint
	if strings.Contains(link, "?") {
		link += "&" + q.Encode()
	} else {
		link += "?" + q.Encode()
	}
	if opts.Open != nil {
		opts.Open(link)
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = SignInTimeout
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var res result
	select {
	case res = <-results:
	case <-waitCtx.Done():
		return Authorization{}, errors.New("the sign-in was not finished in the browser — try again")
	}
	if res.err != nil {
		return Authorization{}, res.err
	}

	auth := Authorization{Resource: resource.Resource, Scopes: scopes, Server: meta, Client: client}
	token, err := tokenRequest(ctx, httpc, auth, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {res.code},
		"redirect_uri":  {client.RedirectURI},
		"code_verifier": {verifier},
	}, "")
	if err != nil {
		return Authorization{}, err
	}
	auth.Token = token
	return auth, nil
}

// Refresh trades the refresh token for a new access token.
func Refresh(ctx context.Context, httpc *http.Client, auth Authorization) (Token, error) {
	if auth.Token.Refresh == "" {
		return Token{}, ErrGrantRevoked
	}
	if httpc == nil {
		httpc = defaultHTTP
	}
	return tokenRequest(ctx, httpc, auth, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {auth.Token.Refresh},
	}, auth.Token.Refresh)
}

// Revoke tells the authorization server to forget the grant. Best effort for
// the caller: a disconnect that could not reach the server has still
// forgotten the token here.
func Revoke(ctx context.Context, httpc *http.Client, auth Authorization) error {
	if auth.Server.RevocationEndpoint == "" {
		return nil
	}
	if httpc == nil {
		httpc = defaultHTTP
	}
	token, hint := auth.Token.Refresh, "refresh_token"
	if token == "" {
		token, hint = auth.Token.Access, "access_token"
	}
	if token == "" {
		return nil
	}
	form := url.Values{"token": {token}, "token_type_hint": {hint}}
	req, err := clientRequest(ctx, auth, auth.Server.RevocationEndpoint, form)
	if err != nil {
		return err
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// tokenRequest posts to the token endpoint. keepRefresh is the refresh token
// to keep when the answer brings none — servers that do not rotate answer a
// refresh with the access token alone.
func tokenRequest(ctx context.Context, httpc *http.Client, auth Authorization, form url.Values, keepRefresh string) (Token, error) {
	if auth.Resource != "" {
		form.Set("resource", auth.Resource)
	}
	req, err := clientRequest(ctx, auth, auth.Server.TokenEndpoint, form)
	if err != nil {
		return Token{}, err
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("could not reach the sign-in server: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
		Error        string `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	if resp.StatusCode/100 != 2 || body.AccessToken == "" {
		if body.Error == "invalid_grant" || body.Error == "invalid_client" {
			return Token{}, ErrGrantRevoked
		}
		return Token{}, fmt.Errorf("the sign-in server refused the token: %s", oauthMessage(raw, resp.Status))
	}
	token := Token{Access: body.AccessToken, Refresh: body.RefreshToken, Scope: body.Scope}
	if body.ExpiresIn > 0 {
		token.Expiry = time.Now().Add(time.Duration(body.ExpiresIn) * time.Second)
	}
	if token.Refresh == "" {
		token.Refresh = keepRefresh
	}
	return token, nil
}

// clientRequest is a form post that says which client is asking, the way the
// registration said to.
func clientRequest(ctx context.Context, auth Authorization, endpoint string, form url.Values) (*http.Request, error) {
	basic := auth.Client.ClientSecret != "" && auth.Client.AuthMethod == "client_secret_basic"
	if !basic {
		form.Set("client_id", auth.Client.ClientID)
		if auth.Client.ClientSecret != "" {
			form.Set("client_secret", auth.Client.ClientSecret)
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if basic {
		req.SetBasicAuth(url.QueryEscape(auth.Client.ClientID), url.QueryEscape(auth.Client.ClientSecret))
	}
	return req, nil
}

func oauthMessage(raw []byte, status string) string {
	var body struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	_ = json.Unmarshal(raw, &body)
	switch {
	case body.Description != "":
		return body.Description
	case body.Error != "":
		return body.Error
	}
	return status
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func randomString(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)[:n]
}

// callbackPage is what the browser shows once the sign-in comes back, so the
// tab says what happened instead of spinning on a request nobody answered.
func callbackPage(w http.ResponseWriter, app string, err error) {
	if app == "" {
		app = "the app"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	msg := "Signed in. You can close this tab and go back to " + html.EscapeString(app) + "."
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		msg = "The sign-in did not finish: " + html.EscapeString(err.Error())
	}
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>%s</title>`+
		`<body style="font:16px system-ui;margin:3rem;max-width:36rem">%s</body>`, html.EscapeString(app), msg)
}
