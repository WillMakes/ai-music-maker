package app

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"aimusicmaker/frontend"
)

// ---------------------------------------------------------------------------
// The application's map of itself (#88)
//
// What the window can do, page by page, joined to what an agent can do, tool
// by tool. The frontend build writes the first half: frontend/scripts/app-map.mjs
// reads navigation.ts for the pages, App.tsx for what each one renders, and
// the import graph for which bindings each one reaches, and drops the result
// into dist — which this binary embeds with the rest of the frontend. This
// file adds the second half at read time: for every binding, the tools that
// reach it, or the reason none does; and every tool with its schema.
//
// Generated at both ends and written by hand at neither, so an agent asked to
// document the application has one call that cannot describe a page the
// build does not have or a tool the server does not offer.
// ---------------------------------------------------------------------------

// appMapFile is where the frontend build writes the map, inside the dist the
// binary embeds (see the frontend package).
const appMapFile = "dist/app-map.json"

// AppUse is one binding as a page or the shell uses it.
type AppUse struct {
	Name string `json:"name"`
	// Via is the source file(s) the call is made from.
	Via []string `json:"via"`
	// Tools are the MCP tools that reach the same binding; empty when none
	// does, in which case Reason says why.
	Tools   []string `json:"tools"`
	Offered bool     `json:"offered"`
	Reason  string   `json:"reason"`
}

// AppView is one page: a ViewId in navigation.ts.
type AppView struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Section is the rail item that stays lit for the page.
	Section string `json:"section"`
	// Nav says where the page is reached from: "primary" or "bottom" for a
	// rail entry, "settings-tab" for a group on the settings page (#116; the
	// name is from when they were tabs), "" for a page opened from another
	// page.
	Nav   string `json:"nav"`
	Label string `json:"label"`
	// Components are the source files a route guard picked for this page.
	Components []string `json:"components"`
	// Files is everything the page's components reach, the shell excluded.
	Files    []string `json:"files"`
	Bindings []AppUse `json:"bindings"`
}

// AppShell is what every page has in common: the rail, the bars, the
// providers, and the bindings they call on every page.
type AppShell struct {
	Files    []string `json:"files"`
	Bindings []AppUse `json:"bindings"`
}

// AppBinding is one exported method on App, seen from both sides.
type AppBinding struct {
	Name string `json:"name"`
	// Views are the pages that call it; Shell says it is called on every page.
	// Both empty for a binding only the CLI or the MCP server reaches.
	Views   []string `json:"views"`
	Shell   bool     `json:"shell"`
	Tools   []string `json:"tools"`
	Offered bool     `json:"offered"`
	Reason  string   `json:"reason"`
}

// AppTool is one MCP tool, with the binding it reaches.
type AppTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Binding     string         `json:"binding"`
	InputSchema map[string]any `json:"inputSchema"`
}

// AppMap is the whole answer.
type AppMap struct {
	GeneratedAt string    `json:"generatedAt"`
	Views       []AppView `json:"views"`
	Shell       AppShell  `json:"shell"`
	// RetiredRoutes are view ids older builds had, mapped to where their
	// content lives now — a bug report's route may still name one.
	RetiredRoutes map[string]string `json:"retiredRoutes"`
	Bindings      []AppBinding      `json:"bindings"`
	Tools         []AppTool         `json:"tools"`
}

// DescribeApp reads the map the build wrote and joins the tool surface to it.
func (a *App) DescribeApp() (AppMap, error) {
	raw, err := frontend.Dist.ReadFile(appMapFile)
	if err != nil {
		return AppMap{}, fmt.Errorf(
			"this build has no app map — `npm run build` in frontend writes it: %w", err)
	}
	var m AppMap
	if err := json.Unmarshal(raw, &m); err != nil {
		return AppMap{}, fmt.Errorf("the app map could not be read: %w", err)
	}

	// Which tools reach which binding, from the tools' own declarations.
	toolsOf := map[string][]string{}
	for _, t := range mcpTools() {
		for _, name := range strings.Fields(t.binding) {
			toolsOf[name] = append(toolsOf[name], t.name)
		}
	}
	fill := func(uses []AppUse) {
		for i := range uses {
			uses[i].Tools = append([]string{}, toolsOf[uses[i].Name]...)
			uses[i].Offered = len(uses[i].Tools) > 0
			uses[i].Reason = mcpNotOffered[uses[i].Name]
			if uses[i].Via == nil {
				uses[i].Via = []string{}
			}
		}
	}

	// Where each binding is used, for the index below.
	viewsOf := map[string][]string{}
	for i := range m.Views {
		view := &m.Views[i]
		if view.Components == nil {
			view.Components = []string{}
		}
		if view.Files == nil {
			view.Files = []string{}
		}
		if view.Bindings == nil {
			view.Bindings = []AppUse{}
		}
		fill(view.Bindings)
		for _, use := range view.Bindings {
			viewsOf[use.Name] = append(viewsOf[use.Name], view.ID)
		}
	}
	if m.Shell.Files == nil {
		m.Shell.Files = []string{}
	}
	if m.Shell.Bindings == nil {
		m.Shell.Bindings = []AppUse{}
	}
	fill(m.Shell.Bindings)
	shell := map[string]bool{}
	for _, use := range m.Shell.Bindings {
		shell[use.Name] = true
	}
	if m.RetiredRoutes == nil {
		m.RetiredRoutes = map[string]string{}
	}

	// The index is every exported method on App, by reflection, so a binding
	// no page and no tool reaches — one only the CLI uses — is still listed
	// rather than being invisible to a report about what the app can do.
	m.Bindings = []AppBinding{}
	appType := reflect.TypeOf(a)
	for i := 0; i < appType.NumMethod(); i++ {
		name := appType.Method(i).Name
		views := append([]string{}, viewsOf[name]...)
		tools := append([]string{}, toolsOf[name]...)
		m.Bindings = append(m.Bindings, AppBinding{
			Name:    name,
			Views:   views,
			Shell:   shell[name],
			Tools:   tools,
			Offered: len(tools) > 0,
			Reason:  mcpNotOffered[name],
		})
	}
	sort.Slice(m.Bindings, func(i, j int) bool { return m.Bindings[i].Name < m.Bindings[j].Name })

	m.Tools = []AppTool{}
	for _, t := range mcpTools() {
		m.Tools = append(m.Tools, AppTool{
			Name: t.name, Description: t.description, Binding: t.binding, InputSchema: t.schema,
		})
	}
	return m, nil
}
