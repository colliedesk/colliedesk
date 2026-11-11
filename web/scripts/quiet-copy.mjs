import { createHash } from 'node:crypto';
import { readdirSync, readFileSync } from 'node:fs';
import { dirname, join, relative, resolve } from 'node:path';
import ts from 'typescript';

// The quiet-copy gate (quiet-copy.test.mjs) reads the TSX of the web app, not a rendered page: a helper line often
// shows only in one state (an error, a locked role, a step of a wizard) that no single render reaches, and reading
// the source finds every one of them in every state. It follows each helper's text back to its message entry
// through imports, local constants and nested objects. A value it cannot know (a name, a count) counts as no
// characters, so the length is a lower bound and a helper whose fixed words pass 60 characters always fails.

export const limit = 60;
// Always-visible helper copy: these classes, any class ending in -lead, -help or -hint, a section description
// (`.section-heading > p`), a note right in a settings card (`.settings-rows > .muted-copy`) and a component's
// hint, help or lead prop. Errors (.error-copy, role="alert") and warnings (.warning-note: a consequence or a
// state to act on) stay visible by the rule, and .sr-only is never seen.
const helperClasses = new Set(['help', 'field-note', 'lead', 'section-lead', 'kb-hint', 'af-help']);
const helperSuffixes = ['-lead', '-help', '-hint'];
const excludedClasses = new Set(['error-copy', 'warning-note', 'sr-only']);
const helperProps = new Set(['hint', 'help', 'lead']);

// sourcesUnder reads every .ts and .tsx file of the app but its tests.
export function sourcesUnder(dir) {
  const files = new Map();
  const walk = (at) => readdirSync(at, { withFileTypes: true }).forEach((entry) => {
    const path = join(at, entry.name);
    if (entry.isDirectory()) walk(path);
    else if (/\.tsx?$/.test(entry.name) && !/\.test\.tsx?$/.test(entry.name) && !entry.name.endsWith('.d.ts')) files.set(path, readFileSync(path, 'utf8'));
  });
  walk(dir);
  return files;
}

// readCeiling reads the keys of the ceiling list: one per line, `#` starts a comment.
export function readCeiling(text) {
  return new Set(text.split('\n').map((line) => line.replace(/(^|\s)#.*$/, '').trim()).filter(Boolean));
}

const isFunction = (node) => ts.isArrowFunction(node) || ts.isFunctionExpression(node) || ts.isFunctionDeclaration(node) || ts.isMethodDeclaration(node);
const unwrap = (node) => (ts.isParenthesizedExpression(node) || ts.isAsExpression(node) || ts.isSatisfiesExpression(node) || ts.isNonNullExpression(node) ? unwrap(node.expression) : node);
const nothing = { text: '', keys: [] };
const join2 = (a, b) => ({ text: a.text + b.text, keys: [...a.keys, ...b.keys] });
const longer = (a, b) => (b.text.length > a.text.length ? b : a);

// scanQuietCopy lists the helper lines longer than the limit in files (path → source); root makes paths relative.
export function scanQuietCopy(files, root) {
  const parsed = new Map();
  const sourceOf = (path) => {
    if (!parsed.has(path)) parsed.set(path, ts.createSourceFile(path, files.get(path), ts.ScriptTarget.Latest, true, path.endsWith('.tsx') ? ts.ScriptKind.TSX : ts.ScriptKind.TS));
    return parsed.get(path);
  };
  const moduleFile = (from, spec) => {
    if (!spec.startsWith('.')) return undefined;
    const base = resolve(dirname(from), spec);
    return [base, `${base}.ts`, `${base}.tsx`, join(base, 'index.ts'), join(base, 'index.tsx')].find((path) => files.has(path) && /\.tsx?$/.test(path));
  };
  const rel = (path) => relative(root, path);

  // declaration finds what a name means at a node: a constant, a function, an import, or a parameter (unknown).
  const declaration = (name, at, file) => {
    for (let node = at; node; node = node.parent) {
      if (isFunction(node) && node.parameters.some((param) => ts.isIdentifier(param.name) && param.name.text === name)) return null;
      const statements = ts.isSourceFile(node) || ts.isBlock(node) || ts.isModuleBlock(node) ? node.statements : undefined;
      for (const statement of statements ?? []) {
        if (ts.isVariableStatement(statement)) {
          const found = statement.declarationList.declarations.find((decl) => ts.isIdentifier(decl.name) && decl.name.text === name);
          if (found?.initializer) return { node: found.initializer, file, key: `${rel(file)}:${name}` };
        }
        if (ts.isFunctionDeclaration(statement) && statement.name?.text === name) return { node: statement, file, key: `${rel(file)}:${name}` };
        if (ts.isSourceFile(node) && ts.isImportDeclaration(statement) && statement.importClause?.namedBindings && ts.isNamedImports(statement.importClause.namedBindings)) {
          const binding = statement.importClause.namedBindings.elements.find((element) => element.name.text === name);
          const target = binding && moduleFile(file, statement.moduleSpecifier.text);
          if (target) return declaration((binding.propertyName ?? binding.name).text, sourceOf(target), target);
        }
      }
    }
    return null;
  };

  // resolveValue follows a name or a property path to the expression that defines it, with its message key.
  const resolveValue = (expr, file, depth = 0) => {
    const node = unwrap(expr);
    if (depth > 20) return null;
    if (ts.isIdentifier(node)) {
      const found = declaration(node.text, node, file);
      if (!found || isFunction(found.node)) return found;
      const value = unwrap(found.node);
      if (ts.isIdentifier(value) || ts.isPropertyAccessExpression(value) || ts.isCallExpression(value)) {
        const deeper = resolveValue(value, found.file, depth + 1);
        if (deeper && deeper.node !== value) return deeper;
      }
      // An object of messages is a key's root; a local constant that picks one keeps the keys of what it picked.
      return { node: value, file: found.file, key: ts.isObjectLiteralExpression(value) ? found.key : null };
    }
    if (ts.isPropertyAccessExpression(node)) return propertyOf(resolveValue(node.expression, file, depth + 1), node.name.text, depth);
    // Object.assign(words, more) keeps the object it was given.
    if (ts.isCallExpression(node) && node.expression.getText() === 'Object.assign' && node.arguments.length) return resolveValue(node.arguments[0], file, depth + 1);
    return { node, file, key: null };
  };

  // propertyOf finds a property of an object of messages, also one it takes from a spread (`{ ...other, more }`).
  const propertyOf = (base, name, depth) => {
    if (!base || !ts.isObjectLiteralExpression(base.node) || depth > 20) return null;
    const property = base.node.properties.find((prop) => prop.name && (ts.isIdentifier(prop.name) || ts.isStringLiteral(prop.name)) && prop.name.text === name);
    if (!property) {
      for (const spread of base.node.properties.filter(ts.isSpreadAssignment).reverse()) {
        const found = propertyOf(resolveValue(spread.expression, base.file, depth + 1), name, depth + 1);
        if (found) return found;
      }
      return null;
    }
    const key = base.key ? `${base.key}.${name}` : null;
    if (ts.isMethodDeclaration(property)) return { node: property, file: base.file, key };
    const value = ts.isShorthandPropertyAssignment(property) ? property.name : unwrap(property.initializer);
    if (ts.isIdentifier(value) || ts.isPropertyAccessExpression(value)) return resolveValue(value, base.file, depth + 1);
    return { node: value, file: base.file, key };
  };

  const returnsOf = (body) => {
    const found = [];
    const visit = (node) => { if (ts.isReturnStatement(node) && node.expression) found.push(node.expression); else if (!isFunction(node)) ts.forEachChild(node, visit); };
    ts.forEachChild(body, visit);
    return found;
  };

  // textOf is the words an expression shows; for a choice, the longer one.
  const textOf = (expr, file, depth = 0) => {
    if (!expr || depth > 30) return nothing;
    const node = unwrap(expr);
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return { text: node.text, keys: [] };
    if (ts.isTemplateExpression(node)) {
      return node.templateSpans.reduce((sum, span) => join2(join2(sum, textOf(span.expression, file, depth + 1)), { text: span.literal.text, keys: [] }), { text: node.head.text, keys: [] });
    }
    if (ts.isConditionalExpression(node)) return longer(textOf(node.whenTrue, file, depth + 1), textOf(node.whenFalse, file, depth + 1));
    if (ts.isBinaryExpression(node)) {
      const op = node.operatorToken.kind;
      if (op === ts.SyntaxKind.PlusToken) return join2(textOf(node.left, file, depth + 1), textOf(node.right, file, depth + 1));
      if (op === ts.SyntaxKind.AmpersandAmpersandToken) return textOf(node.right, file, depth + 1);
      if (op === ts.SyntaxKind.BarBarToken || op === ts.SyntaxKind.QuestionQuestionToken) return longer(textOf(node.left, file, depth + 1), textOf(node.right, file, depth + 1));
      return nothing;
    }
    if (ts.isJsxElement(node) || ts.isJsxFragment(node)) return childrenText(node.children, file, depth + 1);
    if (ts.isCallExpression(node)) {
      const callee = resolveValue(node.expression, file);
      if (!callee || !isFunction(callee.node) || !callee.node.body) return nothing;
      const body = callee.node.body;
      const said = ts.isBlock(body) ? returnsOf(body).map((ret) => textOf(ret, callee.file, depth + 1)).reduce(longer, nothing) : textOf(body, callee.file, depth + 1);
      return callee.key ? { text: said.text, keys: [callee.key] } : said;
    }
    if (ts.isIdentifier(node) || ts.isPropertyAccessExpression(node)) {
      const value = resolveValue(node, file);
      if (!value || value.node === node || isFunction(value.node) || ts.isObjectLiteralExpression(value.node)) return nothing;
      const said = textOf(value.node, value.file, depth + 1);
      return value.key && said.text ? { text: said.text, keys: [value.key] } : said;
    }
    return nothing;
  };

  const childrenText = (children, file, depth) => children.reduce((sum, child) => {
    if (ts.isJsxText(child)) {
      const raw = child.text;
      const text = raw.includes('\n') ? raw.split('\n').map((line) => line.trim()).filter(Boolean).join(' ') : raw;
      return join2(sum, { text, keys: [] });
    }
    if (ts.isJsxExpression(child)) return join2(sum, textOf(child.expression, file, depth));
    if (ts.isJsxElement(child) || ts.isJsxFragment(child)) return join2(sum, childrenText(child.children, file, depth + 1));
    return sum;
  }, nothing);

  const attribute = (opening, name) => opening.attributes.properties.find((attr) => ts.isJsxAttribute(attr) && attr.name.getText() === name);
  const classesOf = (opening) => {
    const attr = attribute(opening, 'className');
    if (!attr?.initializer) return [];
    const words = [];
    const visit = (node) => {
      if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node) || ts.isTemplateHead(node) || ts.isTemplateMiddle(node) || ts.isTemplateTail(node)) words.push(...node.text.split(/\s+/));
      ts.forEachChild(node, visit);
    };
    visit(attr.initializer);
    return words.filter(Boolean);
  };
  const openingOf = (node) => (ts.isJsxElement(node) ? node.openingElement : node);
  const jsxParent = (node) => { for (let at = node.parent; at; at = at.parent) if (ts.isJsxElement(at)) return at; return undefined; };
  const isHelper = (element) => {
    const opening = openingOf(element);
    const classes = classesOf(opening);
    const role = attribute(opening, 'role')?.initializer;
    if (classes.some((name) => excludedClasses.has(name)) || (role && ts.isStringLiteral(role) && role.text === 'alert')) return false;
    if (classes.some((name) => helperClasses.has(name) || helperSuffixes.some((suffix) => name.endsWith(suffix)))) return true;
    const parent = jsxParent(element);
    const parentClasses = parent ? classesOf(parent.openingElement) : [];
    if (opening.tagName.getText() === 'p' && parentClasses.includes('section-heading')) return true;
    return classes.includes('muted-copy') && parentClasses.includes('settings-rows');
  };

  const found = new Map();
  const report = (said, file, node) => {
    const text = said.text.replace(/\s+/g, ' ').trim();
    if (text.length <= limit) return;
    const keys = [...new Set(said.keys)];
    const key = keys.length ? keys.join(' + ') : `text:${createHash('sha1').update(text).digest('hex').slice(0, 12)}`;
    const line = sourceOf(file).getLineAndCharacterOfPosition(node.getStart()).line + 1;
    if (!found.has(key)) found.set(key, { key, length: text.length, text, where: `${rel(file)}:${line}` });
  };
  for (const file of [...files.keys()].filter((path) => path.endsWith('.tsx')).sort()) {
    const visit = (node) => {
      if (ts.isJsxElement(node) && isHelper(node)) report(childrenText(node.children, file, 0), file, node);
      if (ts.isJsxAttribute(node) && helperProps.has(node.name.getText()) && node.initializer) {
        report(ts.isStringLiteral(node.initializer) ? { text: node.initializer.text, keys: [] } : textOf(node.initializer.expression, file), file, node);
      }
      ts.forEachChild(node, visit);
    };
    visit(sourceOf(file));
  }
  return [...found.values()];
}
