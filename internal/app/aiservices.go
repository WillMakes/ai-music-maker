package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"aimusicmaker/internal/platform"
)

// ---------------------------------------------------------------------------
// AI service connections
//
// Which AI provider this app may act as. Each provider connects one of two ways,
// and the difference decides what gets billed:
//
//	Account   the CLI is already signed in. Nothing is stored here; a session
//	          invokes the CLI and it holds and refreshes the credential, so usage
//	          draws on the subscription.
//	API key   a Console key, validated against the provider before it is
//	          accepted, used to call the API directly.
//
// A session started in account mode must be careful to scrub ANTHROPIC_API_KEY
// and OPENAI_API_KEY from the environment it hands the CLI: the CLIs prefer an
// env key over the subscription login, so leaving one in place silently bills
// the wrong thing. envWithout is here for whoever writes that code next.
//
// The key is stored in the app's own database and never returned to the
// frontend — the UI is given a masked form and nothing else. That the key sits
// in plain text under ~/.aimusicmaker (created 0700) is a real tradeoff rather than
// an oversight; it is written down here rather than left to be discovered.
// ---------------------------------------------------------------------------

// AI provider ids.
const (
	// serviceAnthropic is Claude. It has a signed-in CLI (`claude`), so account
	// mode is offered where that is installed.
	serviceAnthropic = "anthropic"
	// serviceOpenAI is ChatGPT. Account mode borrows the `codex` CLI's signed-in
	// session; the key path is also what album artwork is generated on (see
	// artwork.go), which is the one feature here with no CLI equivalent.
	serviceOpenAI = "openai"
	// serviceGrok is xAI's Grok. Connected by API key only — there is no
	// signed-in CLI to borrow — and its API is OpenAI-shaped, so it is verified
	// and called the way OpenAI is.
	serviceGrok = "grok"
	// serviceGemini is Google's Gemini (#26). Account mode borrows the `gemini`
	// CLI's signed-in session; the key path talks to the Generative Language
	// API's OpenAI-compatible endpoint, so it too is verified and called the
	// way OpenAI is.
	serviceGemini = "gemini"
	// serviceAntigravity is Google's Antigravity (#27) — Grok's inverse: it has
	// no API to key into, only the signed-in `agy` CLI to borrow, so account
	// mode is the whole of how it connects.
	serviceAntigravity = "antigravity"
)

// aiServiceIDs is every provider, in the order Settings lists them.
var aiServiceIDs = []string{serviceAnthropic, serviceOpenAI, serviceGemini, serviceAntigravity, serviceGrok}

// Connection modes.
const (
	modeAccount = "account"
	modeAPIKey  = "api_key"
)

const (
	// keyAIServices is the settings key the connections are stored under. One
	// JSON blob rather than a key each: they are read and written together, and
	// the settings table is a key/value store rather than somewhere to model a
	// schema.
	keyAIServices = "ai_services"
	// keyDefaultAIService is which provider new AI work reaches for; empty means
	// "decide from what is connected".
	keyDefaultAIService = "default_ai_service"
	// keyAIModelPrefix + an id is that service's own default model. Per service,
	// since the model names and the right choice differ by provider. Empty means
	// "let the provider use its own".
	keyAIModelPrefix = "ai_model_"
)

// aiConnection is what is persisted for one provider.
type aiConnection struct {
	Mode string `json:"mode"`
	// Key is the API key, in api_key mode. It never leaves the backend.
	Key string `json:"key,omitempty"`
	// Account is what to call this connection in the UI.
	Account string `json:"account"`
	// ConnectedAt is when it was made, for the row's second line.
	ConnectedAt string `json:"connectedAt"`
}

// AIServiceStatus is one provider's row in Settings.
type AIServiceStatus struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Connected is true when this app has a way to act as the provider.
	Connected bool   `json:"connected"`
	Mode      string `json:"mode"`
	Account   string `json:"account"`
	// CLIName is the command an account connection uses, "" where there is none.
	CLIName string `json:"cliName"`
	// CLIFound says that command is on this machine, so account mode is offered
	// only where it can actually work.
	CLIFound bool `json:"cliFound"`
	// CLIPath is where it was found.
	CLIPath     string `json:"cliPath"`
	ConnectedAt string `json:"connectedAt"`
	// KeysURL is where a Console key is created, for the row's link.
	KeysURL string `json:"keysUrl"`
	// InstallURL is where this service's CLI is installed from, for the link
	// shown when the CLI is not on the machine. Empty for a service with no CLI.
	InstallURL string `json:"installUrl"`
	// Default is true for the connected service new AI work reaches for.
	Default bool `json:"default"`
	// Model is this service's own default model, "" to let the provider decide.
	Model string `json:"model"`
	// MaskedKey is the stored key with everything but its ends replaced, so the
	// panel can show *which* key is connected without the key leaving here.
	MaskedKey string `json:"maskedKey"`
}

// aiServiceName is what the provider is called in the UI. The product name
// rather than the company's: the producer connected ChatGPT, and a row reading
// "OpenAI" is a row they have to translate.
func aiServiceName(id string) string {
	switch id {
	case serviceAnthropic:
		return "Claude"
	case serviceOpenAI:
		return "ChatGPT"
	case serviceGrok:
		return "Grok"
	case serviceGemini:
		return "Gemini"
	case serviceAntigravity:
		return "Antigravity"
	}
	return id
}

// aiCLIName is the signed-in command account mode borrows, "" where the provider
// offers none.
func aiCLIName(id string) string {
	switch id {
	case serviceAnthropic:
		return "claude"
	case serviceOpenAI:
		return "codex"
	case serviceGemini:
		return "gemini"
	case serviceAntigravity:
		return "agy"
	}
	return ""
}

func aiKeysURL(id string) string {
	switch id {
	case serviceAnthropic:
		return "https://console.anthropic.com/settings/keys"
	case serviceOpenAI:
		return "https://platform.openai.com/api-keys"
	case serviceGrok:
		return "https://console.x.ai"
	case serviceGemini:
		return "https://aistudio.google.com/apikey"
	}
	return ""
}

func aiInstallURL(id string) string {
	switch id {
	case serviceAnthropic:
		return "https://docs.claude.com/en/docs/claude-code/overview"
	case serviceOpenAI:
		return "https://github.com/openai/codex"
	case serviceGemini:
		return "https://github.com/google-gemini/gemini-cli"
	case serviceAntigravity:
		return "https://antigravity.google/docs/cli"
	}
	return ""
}

// GetAIServices reports every provider.
//
// The default is resolved once, here, and handed to each description — not asked
// for from inside one (#12). See describeAIService.
func (a *App) GetAIServices() []AIServiceStatus {
	stored := a.aiConnections()
	starred := a.defaultAIService()
	out := []AIServiceStatus{}
	for _, id := range aiServiceIDs {
		out = append(out, a.describeAIService(id, stored[id], starred))
	}
	return out
}

// GetAIService reports one, for its own settings page.
func (a *App) GetAIService(id string) AIServiceStatus {
	return a.describeAIService(id, a.aiConnections()[id], a.defaultAIService())
}

// describeAIService is one provider's row.
//
// starred is passed in rather than looked up, and that is the whole of #12. This
// used to end with
//
//	status.Default = status.Connected && a.defaultAIService() == id
//
// and defaultAIService asks connectedAIServices, which asks describeAIService —
// a cycle. Go's && short-circuit was the only thing holding it: with nothing
// connected, status.Connected is false for every provider and defaultAIService
// is never reached, so the panel drew correctly and the bug was invisible.
// Connecting one provider opened the guard and the call recursed until the stack
// gave out, which took the answer and the whole AI panel with it — "when an AI
// connection is made, the displays go away".
//
// So the question "which one is starred" is answered above this function, once,
// by the two bindings that need it. Nothing on this path may ask again.
func (a *App) describeAIService(id string, conn aiConnection, starred string) AIServiceStatus {
	status := AIServiceStatus{
		ID:          id,
		Name:        aiServiceName(id),
		Mode:        conn.Mode,
		Account:     conn.Account,
		CLIName:     aiCLIName(id),
		ConnectedAt: conn.ConnectedAt,
		KeysURL:     aiKeysURL(id),
		InstallURL:  aiInstallURL(id),
		Model:       a.aiServiceModel(id),
		MaskedKey:   maskKey(conn.Key),
	}
	if status.CLIName != "" {
		status.CLIPath, status.CLIFound = lookUpCommand(status.CLIName)
	}
	status.Connected = serviceConnected(id, conn)
	status.Default = status.Connected && starred == id
	return status
}

// serviceConnected says whether this app can currently act as a provider.
//
// Its own function because two callers need the answer and only one of them
// wants a whole row: connectedAIServices asks this directly, which is what keeps
// it off describeAIService and out of the cycle described above (#12).
//
// Connected in account mode means the CLI is still there. A machine that has had
// the CLI uninstalled since is not connected, whatever the stored blob says, and
// reporting otherwise would offer a run that cannot start.
func serviceConnected(id string, conn aiConnection) bool {
	switch conn.Mode {
	case modeAccount:
		cli := aiCLIName(id)
		if cli == "" {
			return false
		}
		_, found := lookUpCommand(cli)
		return found
	case modeAPIKey:
		return conn.Key != ""
	}
	return false
}

// maskKey shows enough of a key to recognise it and not enough to use it.
// The first four and last four characters: the prefix is what says which
// provider and which kind of key it is, and the suffix is what the producer
// compares against the Console list.
func maskKey(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 12 {
		return strings.Repeat("•", len(key))
	}
	return key[:4] + strings.Repeat("•", 8) + key[len(key)-4:]
}

// defaultAIService is the chosen provider, resolved to one that can actually
// answer: a stored choice that is still connected stands, otherwise the first
// connected one, otherwise Anthropic — the interviewer's provider and the safe
// floor.
func (a *App) defaultAIService() string {
	stored := a.aiSetting(keyDefaultAIService)
	connected := a.connectedAIServices()
	for _, id := range connected {
		if id == stored {
			return stored
		}
	}
	if len(connected) > 0 {
		return connected[0]
	}
	if stored != "" {
		return stored
	}
	return serviceAnthropic
}

// connectedAIServices is every provider this app can currently act as, in the
// order Settings lists them.
func (a *App) connectedAIServices() []string {
	out := []string{}
	stored := a.aiConnections()
	for _, id := range aiServiceIDs {
		// serviceConnected rather than describeAIService: building a whole row
		// here is what closed the cycle that #12 was (see describeAIService), and
		// the only thing wanted is the flag.
		if serviceConnected(id, stored[id]) {
			out = append(out, id)
		}
	}
	return out
}

// defaultAIModel is the model to run: the default service's own, or "" to let
// the provider use its own.
func (a *App) defaultAIModel() string {
	return a.aiServiceModel(a.defaultAIService())
}

func (a *App) aiServiceModel(id string) string {
	return a.aiSetting(keyAIModelPrefix + id)
}

// SetDefaultAIService stars a provider.
func (a *App) SetDefaultAIService(id string) ([]AIServiceStatus, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	if err := a.store.setSetting(keyDefaultAIService, id); err != nil {
		return nil, err
	}
	a.store.changed(scopeAIServices)
	return a.GetAIServices(), nil
}

// SetAIServiceModel names the model a provider runs, "" to let it choose.
func (a *App) SetAIServiceModel(id, model string) (AIServiceStatus, error) {
	if a.store == nil {
		return AIServiceStatus{}, errNoStore
	}
	if err := a.store.setSetting(keyAIModelPrefix+id, strings.TrimSpace(model)); err != nil {
		return AIServiceStatus{}, err
	}
	a.store.changed(scopeAIServices)
	return a.GetAIService(id), nil
}

// ConnectAIAccount records that this app may use the provider's signed-in CLI.
//
// Nothing is verified beyond the CLI being present. Whether it is *signed in* is
// not answerable without running it, and running somebody's CLI to find out is a
// side effect nobody asked for on a settings page — a session that turns out not
// to be signed in fails with the CLI's own message, which is more use than
// anything this could invent.
func (a *App) ConnectAIAccount(id string) (AIServiceStatus, error) {
	if a.store == nil {
		return AIServiceStatus{}, errNoStore
	}
	cli := aiCLIName(id)
	if cli == "" {
		return AIServiceStatus{}, fmt.Errorf(
			"%s has no signed-in CLI to borrow — connect it with an API key instead",
			aiServiceName(id))
	}
	if _, found := lookUpCommand(cli); !found {
		return AIServiceStatus{}, fmt.Errorf(
			"%s was not found on this machine", cli)
	}
	conns := a.aiConnections()
	conns[id] = aiConnection{
		Mode:        modeAccount,
		Account:     cli + " (signed in)",
		ConnectedAt: nowStamp(),
	}
	if err := a.saveAIConnections(conns); err != nil {
		return AIServiceStatus{}, err
	}
	return a.GetAIService(id), nil
}

// ConnectAIKey validates a key against the provider and stores it.
//
// Validated before it is stored, and stored only if it worked. A key saved
// without being checked is a key that fails at the moment somebody is halfway
// through an interview, with an error from an HTTP call rather than from the
// settings page where the mistake was made.
func (a *App) ConnectAIKey(id, key string) (AIServiceStatus, error) {
	if a.store == nil {
		return AIServiceStatus{}, errNoStore
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return AIServiceStatus{}, fmt.Errorf("no key given")
	}
	account, err := verifyAIKey(id, key)
	if err != nil {
		return AIServiceStatus{}, err
	}
	conns := a.aiConnections()
	conns[id] = aiConnection{
		Mode:        modeAPIKey,
		Key:         key,
		Account:     account,
		ConnectedAt: nowStamp(),
	}
	if err := a.saveAIConnections(conns); err != nil {
		return AIServiceStatus{}, err
	}
	return a.GetAIService(id), nil
}

// DisconnectAI forgets a connection.
func (a *App) DisconnectAI(id string) (AIServiceStatus, error) {
	if a.store == nil {
		return AIServiceStatus{}, errNoStore
	}
	conns := a.aiConnections()
	delete(conns, id)
	if err := a.saveAIConnections(conns); err != nil {
		return AIServiceStatus{}, err
	}
	return a.GetAIService(id), nil
}

// ListAIModels asks the provider what it offers, for the model picker.
//
// Asked rather than hard-coded, because a list written into this build is a list
// that is wrong by the next model release — and being wrong here means the
// producer cannot select a model that exists. Account mode has no key to ask
// with, so it answers empty and the picker offers a free-text field instead.
func (a *App) ListAIModels(id string) ([]string, error) {
	key := a.aiKey(id)
	if key == "" {
		return []string{}, nil
	}
	return listAIModels(id, key)
}

// ---------------------------------------------------------------------------
// Verification
// ---------------------------------------------------------------------------

// aiVerifyHTTP is bounded, and a variable so tests can point it at a stub — the
// alternative is a test suite that fails when the machine is offline.
var aiVerifyHTTP = &http.Client{Timeout: 20 * time.Second}

// verifyAIKey calls the cheapest authenticated endpoint each provider has and
// reports what to call the connection.
//
// The models list, in all three cases: it is a GET, it costs no tokens, and a
// 401 from it means exactly what a 401 from anything else would. Sending a
// one-token completion instead would work and would bill the producer for
// pressing Connect.
func verifyAIKey(id, key string) (string, error) {
	req, err := modelsRequest(id, key)
	if err != nil {
		return "", err
	}
	resp, err := aiVerifyHTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not reach %s: %w", aiServiceName(id), err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return "", fmt.Errorf("%s did not accept that key", aiServiceName(id))
	case resp.StatusCode >= 300:
		return "", fmt.Errorf("%s answered %s", aiServiceName(id), resp.Status)
	}
	return aiServiceName(id) + " API key", nil
}

// modelsRequest builds the models-list call for a provider. Anthropic takes the
// key in x-api-key with a version header; the OpenAI-shaped two take a bearer.
func modelsRequest(id, key string) (*http.Request, error) {
	var url string
	switch id {
	case serviceAnthropic:
		url = "https://api.anthropic.com/v1/models"
	case serviceOpenAI:
		url = "https://api.openai.com/v1/models"
	case serviceGrok:
		url = "https://api.x.ai/v1/models"
	case serviceGemini:
		// The OpenAI-compatibility surface, so the one decode in listAIModels
		// covers this provider too.
		url = "https://generativelanguage.googleapis.com/v1beta/openai/models"
	case serviceAntigravity:
		// No API and so no key to check (#27): the refusal here is what turns a
		// pasted key into a message rather than a stored credential that can
		// never work.
		return nil, fmt.Errorf(
			"Antigravity connects through its signed-in CLI — there is no API key to add")
	default:
		return nil, fmt.Errorf("unknown service %q", id)
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if id == serviceAnthropic {
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	return req, nil
}

// listAIModels reads the ids out of a provider's models list. All three answer
// {"data":[{"id":…}]} — Anthropic's Messages API adopted the OpenAI shape for
// this endpoint — so one decode covers them.
func listAIModels(id, key string) ([]string, error) {
	req, err := modelsRequest(id, key)
	if err != nil {
		return nil, err
	}
	resp, err := aiVerifyHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s answered %s", aiServiceName(id), resp.Status)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := []string{}
	for _, m := range body.Data {
		if m.ID == "" {
			continue
		}
		// Gemini's compatibility surface answers ids as "models/gemini-…"; the
		// chat endpoint takes them bare, so the picker offers them bare (#26).
		out = append(out, strings.TrimPrefix(m.ID, "models/"))
	}
	sort.Strings(out)
	return out, nil
}

// ---------------------------------------------------------------------------
// Storage
// ---------------------------------------------------------------------------

func (a *App) aiSetting(key string) string {
	if a.store == nil {
		return ""
	}
	value, err := a.store.setting(key)
	if err != nil {
		return ""
	}
	return value
}

// aiConnections reads the stored blob. Always a usable map: a blob this app
// cannot decode is one it wrote wrong, and answering "nothing is connected" lets
// the producer connect again, where returning an error would leave the panel
// permanently broken with no way to clear it.
func (a *App) aiConnections() map[string]aiConnection {
	out := map[string]aiConnection{}
	raw := a.aiSetting(keyAIServices)
	if strings.TrimSpace(raw) == "" {
		return out
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return map[string]aiConnection{}
	}
	return out
}

func (a *App) saveAIConnections(conns map[string]aiConnection) error {
	if a.store == nil {
		return errNoStore
	}
	encoded, err := json.Marshal(conns)
	if err != nil {
		return err
	}
	if err := a.store.setSetting(keyAIServices, string(encoded)); err != nil {
		return err
	}
	a.store.changed(scopeAIServices)
	return nil
}

// aiKey is one provider's stored key, "" in account mode. Backend only — no
// binding returns this.
func (a *App) aiKey(id string) string {
	return a.aiConnections()[id].Key
}

// ---------------------------------------------------------------------------
// The machine
// ---------------------------------------------------------------------------

// lookUpCommand finds a command on PATH, including a PATH changed since this
// app started (see platform.LookUp).
//
// A variable, so tests can say what is installed. Without it every test of a
// connection depends on whether the machine running it happens to have the
// Claude CLI.
var lookUpCommand = platform.LookUp

// envWithout returns the current environment with the named variables removed.
//
// For account mode: the Claude and Codex CLIs prefer an API key in the
// environment over the subscription they are signed in with, so handing them an
// inherited ANTHROPIC_API_KEY silently bills the Console account the producer
// deliberately did not connect.
func envWithout(names ...string) []string {
	drop := map[string]bool{}
	for _, n := range names {
		drop[n] = true
	}
	out := []string{}
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if found && drop[name] {
			continue
		}
		out = append(out, entry)
	}
	return out
}
