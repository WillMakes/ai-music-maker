// The application's map, read off its source (#88).
//
// Every page of this app is a ViewId in navigation.ts, rendered by a component
// App.tsx picks behind a `current.view === '…'` guard, and every backend
// function a page can call is a named import from wailsjs/go/app/App
// somewhere under that component. This script reads all three — the view
// table, the guards, the import graph — and writes dist/app-map.json: which
// pages exist, what each one renders, and which bindings each one reaches.
//
// Written at build time and never by hand, for the same reason the MCP server
// lists its tools from the tools themselves: a map somebody maintains is a map
// that is right on the day it was drawn. It runs after vite, into dist, so the
// binary embeds it with the rest of the frontend and `describe_app` can answer
// from it with the window closed. A page with no component, or an import that
// resolves to nothing, fails the build — a map with a hole in it is worse than
// no map, because it is believed.
//
// The graph is followed symbol by symbol, not file by file. The player bar
// imports one small `Cover` component out of the artist page's file; a
// file-level walk would conclude that every page runs the artist interview,
// because the artist page's file does. Following only the export that was
// imported, and only what that export references, keeps the map about what a
// page can do rather than about how the source happens to be divided.
//
// The parser is TypeScript's own (already a devDependency): the alternative is
// regular expressions over TSX, which work until the first multi-line import.

import fs from 'node:fs'
import path from 'node:path'
import {fileURLToPath} from 'node:url'
import ts from 'typescript'

const frontend = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const src = path.join(frontend, 'src')
const out = path.join(frontend, 'dist', 'app-map.json')

// BINDINGS marks an import of the generated bindings module. Its named imports
// are the App methods the importing file can call.
const BINDINGS = Symbol('bindings')

// EVERYTHING asks for a whole file rather than named exports of it: the entry
// point, whose top-level statements are the program, and any file whose
// requested export could not be found — over-reading it is the safe failure.
const EVERYTHING = '*'

// ---------------------------------------------------------------------------
// Modules
// ---------------------------------------------------------------------------

const modules = new Map()

/** A parsed source file with its runtime imports resolved and its top level indexed. */
function load(file) {
  if (modules.has(file)) return modules.get(file)
  const text = fs.readFileSync(file, 'utf8')
  const kind = file.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS
  const sf = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, kind)
  const mod = {file, sf, imports: [], locals: new Map(), exports: new Map()}
  for (const st of sf.statements) {
    if (!ts.isImportDeclaration(st) || !st.importClause) continue
    const clause = st.importClause
    // `import type {Route} from '../App'` pulls no code: following it would
    // make every view that names a Route depend on the whole shell.
    if (clause.isTypeOnly) continue
    const names = []
    if (clause.name) names.push({local: clause.name.text, imported: 'default'})
    const bindings = clause.namedBindings
    if (bindings && ts.isNamedImports(bindings)) {
      for (const el of bindings.elements) {
        if (el.isTypeOnly) continue
        names.push({local: el.name.text, imported: (el.propertyName ?? el.name).text})
      }
    } else if (bindings && ts.isNamespaceImport(bindings)) {
      names.push({local: bindings.name.text, imported: EVERYTHING})
    }
    if (names.length === 0) continue
    const resolved = resolve(file, st.moduleSpecifier.text)
    if (resolved === null) continue
    mod.imports.push({resolved, names})
  }
  indexTopLevel(mod)
  modules.set(file, mod)
  return mod
}

/**
 * Where a relative import points: a source file, the bindings module, or null
 * for a package, a stylesheet or the runtime — none of which reach a binding.
 */
function resolve(from, spec) {
  if (!spec.startsWith('.')) return null
  const base = path.resolve(path.dirname(from), spec)
  if (/[\\/]wailsjs[\\/]go[\\/]app[\\/]App$/.test(base)) return BINDINGS
  if (base.includes(`${path.sep}wailsjs${path.sep}`)) return null
  if (/\.(css|png|jpg|svg)$/.test(spec)) return null
  for (const candidate of [base, `${base}.ts`, `${base}.tsx`,
    path.join(base, 'index.ts'), path.join(base, 'index.tsx')]) {
    if (fs.existsSync(candidate) && fs.statSync(candidate).isFile()) return candidate
  }
  throw new Error(`${rel(from)} imports '${spec}', which resolves to nothing`)
}

const rel = (file) => path.relative(src, file).split(path.sep).join('/')

const hasModifier = (st, kind) => (st.modifiers ?? []).some((m) => m.kind === kind)
const isExported = (st) => hasModifier(st, ts.SyntaxKind.ExportKeyword)

/** Every top-level name and the statement that declares it, and which are exported as what. */
function indexTopLevel(mod) {
  const {locals, exports} = mod
  for (const st of mod.sf.statements) {
    if ((ts.isFunctionDeclaration(st) || ts.isClassDeclaration(st) || ts.isEnumDeclaration(st)) && st.name) {
      locals.set(st.name.text, st)
      if (isExported(st)) {
        exports.set(hasModifier(st, ts.SyntaxKind.DefaultKeyword) ? 'default' : st.name.text, st)
      }
    } else if (ts.isVariableStatement(st)) {
      for (const decl of st.declarationList.declarations) {
        for (const name of boundNames(decl.name)) {
          locals.set(name, st)
          if (isExported(st)) exports.set(name, st)
        }
      }
    } else if (ts.isExportAssignment(st)) {
      exports.set('default', st)
    }
  }
  // `export {a, b}` after the fact, of names declared above.
  for (const st of mod.sf.statements) {
    if (ts.isExportDeclaration(st) && !st.moduleSpecifier && st.exportClause && ts.isNamedExports(st.exportClause)) {
      for (const el of st.exportClause.elements) {
        const local = locals.get((el.propertyName ?? el.name).text)
        if (local) exports.set(el.name.text, local)
      }
    }
  }
}

/** The names a binding pattern introduces: `x`, `{a, b}`, `[c]`. */
function boundNames(name) {
  if (ts.isIdentifier(name)) return [name.text]
  const names = []
  for (const el of name.elements) {
    if (ts.isBindingElement(el)) names.push(...boundNames(el.name))
  }
  return names
}

// ---------------------------------------------------------------------------
// The view table
// ---------------------------------------------------------------------------

/** A literal expression as a value; anything that is not one (an icon) is undefined. */
function literal(node) {
  if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return node.text
  if (ts.isNumericLiteral(node)) return Number(node.text)
  if (node.kind === ts.SyntaxKind.TrueKeyword) return true
  if (node.kind === ts.SyntaxKind.FalseKeyword) return false
  if (ts.isAsExpression(node) || ts.isSatisfiesExpression(node)) return literal(node.expression)
  if (ts.isArrayLiteralExpression(node)) return node.elements.map(literal)
  if (ts.isObjectLiteralExpression(node)) {
    const value = {}
    for (const prop of node.properties) {
      if (!ts.isPropertyAssignment(prop)) continue
      const key = ts.isIdentifier(prop.name) || ts.isStringLiteral(prop.name)
        ? prop.name.text : null
      if (key !== null) value[key] = literal(prop.initializer)
    }
    return value
  }
  return undefined
}

function readNavigation() {
  const mod = load(path.join(src, 'navigation.ts'))
  const consts = {}
  let viewIds = null
  // Functions like isSettingsView(view) are a prefix test on the id; the
  // prefix is read off the body rather than assumed, so a renamed section
  // still narrows the guard correctly.
  const prefixGuards = {}
  for (const st of mod.sf.statements) {
    if (ts.isTypeAliasDeclaration(st) && st.name.text === 'ViewId' && ts.isUnionTypeNode(st.type)) {
      viewIds = st.type.types
        .filter((t) => ts.isLiteralTypeNode(t) && ts.isStringLiteral(t.literal))
        .map((t) => t.literal.text)
    }
    if (ts.isVariableStatement(st)) {
      for (const decl of st.declarationList.declarations) {
        if (!ts.isIdentifier(decl.name) || !decl.initializer) continue
        if (ts.isArrowFunction(decl.initializer)) {
          const prefix = startsWithPrefix(decl.initializer)
          if (prefix !== null) prefixGuards[decl.name.text] = prefix
          continue
        }
        consts[decl.name.text] = literal(decl.initializer)
      }
    }
  }
  if (!viewIds || viewIds.length === 0) throw new Error('navigation.ts declares no ViewId')
  return {viewIds, consts, prefixGuards}
}

function startsWithPrefix(fn) {
  let prefix = null
  const visit = (node) => {
    if (ts.isCallExpression(node) && ts.isPropertyAccessExpression(node.expression) &&
      node.expression.name.text === 'startsWith' && node.arguments.length === 1 &&
      ts.isStringLiteral(node.arguments[0])) {
      prefix = node.arguments[0].text
    }
    ts.forEachChild(node, visit)
  }
  visit(fn)
  return prefix
}

// ---------------------------------------------------------------------------
// Guards
//
// A set of view ids, or null for "every view" — the state of a component
// rendered with no condition on the route at all.
// ---------------------------------------------------------------------------

const nav = readNavigation()
const VIEW_IDS = nav.viewIds
const isView = (s) => VIEW_IDS.includes(s)

const intersect = (a, b) => (a === null ? b : b === null ? a : new Set([...a].filter((v) => b.has(v))))
const union = (a, b) => (a === null || b === null ? null : new Set([...a, ...b]))
const complement = (a) => (a === null ? null : new Set(VIEW_IDS.filter((v) => !a.has(v))))
const guardKey = (g) => (g === null ? '*' : [...g].sort().join(','))

/** What a condition says about the route, when it says anything. */
function evalGuard(expr) {
  if (ts.isParenthesizedExpression(expr)) return evalGuard(expr.expression)
  if (ts.isPrefixUnaryExpression(expr) && expr.operator === ts.SyntaxKind.ExclamationToken) {
    return complement(evalGuard(expr.operand))
  }
  if (ts.isCallExpression(expr) && ts.isIdentifier(expr.expression)) {
    const prefix = nav.prefixGuards[expr.expression.text]
    if (prefix !== undefined) return new Set(VIEW_IDS.filter((v) => v.startsWith(prefix)))
    return null
  }
  if (!ts.isBinaryExpression(expr)) return null
  const op = expr.operatorToken.kind
  const K = ts.SyntaxKind
  if (op === K.AmpersandAmpersandToken) return intersect(evalGuard(expr.left), evalGuard(expr.right))
  if (op === K.BarBarToken) return union(evalGuard(expr.left), evalGuard(expr.right))
  const lit = [expr.left, expr.right].find((s) => ts.isStringLiteral(s) && isView(s.text))
  if (!lit) return null
  if (op === K.EqualsEqualsEqualsToken || op === K.EqualsEqualsToken) return new Set([lit.text])
  if (op === K.ExclamationEqualsEqualsToken || op === K.ExclamationEqualsToken) return complement(new Set([lit.text]))
  return null
}

/** The guard a node sits under, climbing from it to the boundary. */
function guardOf(node, boundary) {
  let guard = null
  let prev = node
  for (let p = node.parent; p && p !== boundary; prev = p, p = p.parent) {
    if (ts.isBinaryExpression(p) && p.operatorToken.kind === ts.SyntaxKind.AmpersandAmpersandToken && prev === p.right) {
      guard = intersect(guard, evalGuard(p.left))
    } else if (ts.isConditionalExpression(p)) {
      if (prev === p.whenTrue) guard = intersect(guard, evalGuard(p.condition))
      else if (prev === p.whenFalse) guard = intersect(guard, complement(evalGuard(p.condition)))
    }
  }
  return guard
}

// ---------------------------------------------------------------------------
// References
// ---------------------------------------------------------------------------

/** Whether an identifier is a use of a name, rather than the name of something. */
function isReference(id) {
  const p = id.parent
  if (ts.isPropertyAccessExpression(p) && p.name === id) return false
  if (ts.isPropertyAssignment(p) && p.name === id) return false
  if ((ts.isVariableDeclaration(p) || ts.isFunctionDeclaration(p) || ts.isParameter(p) ||
    ts.isBindingElement(p) || ts.isClassDeclaration(p)) && p.name === id) return false
  if (ts.isJsxAttribute(p) && p.name === id) return false
  return true
}

/**
 * Every name the requested top-level statements of a file refer to, with the
 * route guard each is used under — null when it is used somewhere unguarded.
 * A reference to another top-level declaration in the same file is followed,
 * carrying the guard: App.tsx's ServiceDetailRoute is a local function, and
 * the guard on its call site is the guard on everything it renders.
 */
function references(mod, statements) {
  const refs = new Map()
  // Names used as JSX tags, which is what makes a guarded import a component
  // of the page rather than a value it happens to read under the same guard.
  const tags = new Set()
  const walked = new Set()
  const queue = statements.map((st) => ({st, guard: null}))
  while (queue.length > 0) {
    const {st, guard: inherited} = queue.shift()
    const key = `${st.pos}|${guardKey(inherited)}`
    if (walked.has(key)) continue
    walked.add(key)
    const visit = (node) => {
      if (ts.isIdentifier(node) && isReference(node)) {
        const guard = intersect(inherited, guardOf(node, st))
        refs.set(node.text, refs.has(node.text) ? union(refs.get(node.text), guard) : guard)
        if ((ts.isJsxOpeningElement(node.parent) || ts.isJsxSelfClosingElement(node.parent)) &&
          node.parent.tagName === node) {
          tags.add(node.text)
        }
        const local = mod.locals.get(node.text)
        if (local && local !== st) queue.push({st: local, guard})
      }
      ts.forEachChild(node, visit)
    }
    visit(st)
  }
  return {refs, tags}
}

// ---------------------------------------------------------------------------
// Attribution
// ---------------------------------------------------------------------------

const empty = () => ({files: new Set(), bindings: new Map(), components: new Set()})
const memo = new Map()
const inProgress = new Set()

/**
 * What each of `views` reaches through the named exports of a file: the files,
 * the bindings (with the file each is called from), and the components a route
 * guard picked along the way.
 */
function attribute(file, names, views) {
  const key = `${file}|${[...names].sort().join(',')}|${[...views].sort().join(',')}`
  if (memo.has(key)) return memo.get(key)
  const result = new Map([...views].map((v) => [v, empty()]))
  if (inProgress.has(key)) return result
  inProgress.add(key)

  const mod = load(file)
  for (const v of views) result.get(v).files.add(rel(file))

  let statements
  if (names.has(EVERYTHING) || [...names].some((n) => !mod.exports.has(n))) {
    statements = [...mod.sf.statements]
  } else {
    statements = [...new Set([...names].map((n) => mod.exports.get(n)))]
  }
  const {refs, tags} = references(mod, statements)

  for (const imp of mod.imports) {
    for (const {local, imported} of imp.names) {
      if (!refs.has(local)) continue
      const guard = refs.get(local)
      const sub = guard === null ? views : intersect(views, guard)
      if (sub.size === 0) continue
      if (imp.resolved === BINDINGS) {
        for (const v of sub) {
          const r = result.get(v)
          if (!r.bindings.has(imported)) r.bindings.set(imported, new Set())
          r.bindings.get(imported).add(rel(file))
        }
        continue
      }
      const below = attribute(imp.resolved, new Set([imported]), sub)
      for (const v of sub) {
        const r = result.get(v)
        const b = below.get(v)
        if (guard !== null && tags.has(local)) r.components.add(rel(imp.resolved))
        for (const f of b.files) r.files.add(f)
        for (const c of b.components) r.components.add(c)
        for (const [name, via] of b.bindings) {
          if (!r.bindings.has(name)) r.bindings.set(name, new Set())
          for (const f of via) r.bindings.get(name).add(f)
        }
      }
    }
  }
  inProgress.delete(key)
  memo.set(key, result)
  return result
}

// ---------------------------------------------------------------------------
// The map
// ---------------------------------------------------------------------------

const all = attribute(path.join(src, 'main.tsx'), new Set([EVERYTHING]), new Set(VIEW_IDS))

// The shell is what every view has in common — main.tsx, App.tsx, the rail,
// the bars, the providers. Derived rather than named, so a component promoted
// to the shell moves there on the next build without anyone editing a list.
const shellFiles = VIEW_IDS.map((v) => all.get(v).files)
  .reduce((acc, files) => new Set([...acc].filter((f) => files.has(f))))
const shellBindings = VIEW_IDS.map((v) => new Set(all.get(v).bindings.keys()))
  .reduce((acc, names) => new Set([...acc].filter((n) => names.has(n))))
// Where the shell itself calls each of its bindings from: the files every view
// reaches it through. A view still owns a shell binding that it also calls
// from somewhere else (#129) — the setup guide connects MCP clients from the
// shell, and that must not take Connect off the AI tab, whose own panel calls
// it too.
const shellVia = new Map([...shellBindings].map((name) => [
  name,
  VIEW_IDS.map((v) => all.get(v).bindings.get(name))
    .reduce((acc, via) => new Set([...acc].filter((f) => via.has(f)))),
]))
const ownsBinding = (r, name) =>
  !shellBindings.has(name) || [...r.bindings.get(name)].some((f) => !shellVia.get(name).has(f))

const sorted = (set) => [...set].sort()
const bindingsOf = (r, keep) => sorted(r.bindings.keys())
  .filter(keep)
  .map((name) => ({name, via: sorted(r.bindings.get(name))}))

const navKind = (id) => {
  const inList = (name) => (nav.consts[name] ?? []).some((e) => e && e.id === id)
  if (inList('PRIMARY_NAV')) return 'primary'
  if (inList('BOTTOM_NAV')) return 'bottom'
  if (inList('SETTINGS_TABS')) return 'settings-tab'
  return ''
}
const navLabel = (id) => {
  for (const name of ['PRIMARY_NAV', 'BOTTOM_NAV', 'SETTINGS_TABS']) {
    const entry = (nav.consts[name] ?? []).find((e) => e && e.id === id)
    if (entry) return entry.label
  }
  return ''
}

const views = VIEW_IDS.map((id) => {
  const r = all.get(id)
  if (r.components.size === 0) {
    throw new Error(`view '${id}' is declared in navigation.ts but nothing renders it`)
  }
  return {
    id,
    title: nav.consts.ROUTE_TITLES?.[id] ?? '',
    section: nav.consts.SECTION_OF?.[id] ?? '',
    nav: navKind(id),
    label: navLabel(id),
    components: sorted(r.components),
    files: sorted(r.files).filter((f) => !shellFiles.has(f)),
    bindings: bindingsOf(r, (name) => ownsBinding(r, name)),
  }
})

const map = {
  generatedAt: new Date().toISOString(),
  views,
  shell: {
    files: sorted(shellFiles),
    bindings: bindingsOf(all.get(VIEW_IDS[0]), (name) => shellBindings.has(name)),
  },
  retiredRoutes: nav.consts.RETIRED_ROUTES ?? {},
  filesScanned: modules.size,
}

fs.mkdirSync(path.dirname(out), {recursive: true})
fs.writeFileSync(out, JSON.stringify(map, null, 2) + '\n')
const bindingCount = new Set([...views.flatMap((v) => v.bindings.map((b) => b.name)),
  ...map.shell.bindings.map((b) => b.name)]).size
console.log(`app-map: ${views.length} views, ${bindingCount} bindings, ${modules.size} files → ${path.relative(frontend, out)}`)
