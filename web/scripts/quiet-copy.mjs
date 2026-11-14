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
// Always-visible helper copy: these classes (.muted-copy anywhere), any class ending in -lead, -help or -hint, a
// section description (`.section-heading > p`) and a component's hint, help, lead, description, note, detail or
// summary prop. Errors (.error-copy, role="alert") and warnings (.warning-note: a consequence or a state to act on)
// stay visible by the rule, and .sr-only is never seen. Any other line the rule keeps visible says so with
// data-quiet: "state" (a state the person must act on), "consequence" (right before an irreversible action) or
// "value" (a value to copy).
const helperClasses = new Set(['help', 'field-note', 'lead', 'section-lead', 'kb-hint', 'af-help', 'muted-copy']);
const helperSuffixes = ['-lead', '-help', '-hint'];
const excludedClasses = new Set(['error-copy', 'warning-note', 'sr-only']);
const helperProps = new Set(['hint', 'help', 'lead', 'description', 'note', 'detail', 'summary']);
const keptLines = new Set(['state', 'consequence', 'value']);

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

// readCeiling reads the ceiling list into key → length: one line per helper, its key, a tab and its length in
// characters; `#` starts a comment.
export function readCeiling(text) {
  return new Map(text.split('\n').map((line) => line.replace(/(^|\s)#.*$/, '').trim()).filter(Boolean).map((line) => {
    const [key, length, ...rest] = line.split('\t');
    if (!/^\d+$/.test(length ?? '') || rest.length) throw new Error(`quiet-copy ceiling: "${line}" needs its key, a tab and its length`);
    return [key, Number(length)];
  }));
}

// checkCeiling compares the helper lines found with the ceiling list (readCeiling). A listed line keeps its recorded
// length or less, and a shorter one lowers it, so the list only shrinks; a line not on the list fails.
export function checkCeiling(found, ceiling) {
  const problems = [];
  for (const item of found) {
    const recorded = ceiling.get(item.key);
    if (recorded === undefined) problems.push(`${item.key} is new (${item.where}, ${item.length} characters): ${item.text}`);
    else if (item.length > recorded) problems.push(`${item.key} is ${item.length} characters (${item.where}), over its ${recorded} on the list: ${item.text}`);
    else if (item.length < recorded) problems.push(`${item.key} is ${item.length} characters now (${item.where}): write ${item.length} for it on the list`);
  }
  const current = new Set(found.map((item) => item.key));
  for (const key of ceiling.keys()) if (!current.has(key)) problems.push(`${key} is ${limit} characters or less now, or gone: delete it from the list`);
  return problems;
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

  // bound finds a name in a parameter or a destructuring (`{ note = m.why }`): its default, or null when it has none.
  const bound = (name, binding, file) => {
    if (ts.isIdentifier(binding)) return binding.text === name ? undefined : false;
    for (const element of binding.elements) {
      if (ts.isOmittedExpression(element)) continue;
      const found = bound(name, element.name, file);
      if (found === undefined) return element.initializer ? { node: element.initializer, file, key: `${rel(file)}:${name}` } : null;
      if (found !== false) return found;
    }
    return false;
  };
  // declaration finds what a name means at a node: a constant, a function, an import, a parameter's default, or
  // null for a parameter without one (unknown). A parameter or a destructured constant hides an outer name.
  const declaration = (name, at, file) => {
    for (let node = at; node; node = node.parent) {
      if (isFunction(node)) {
        for (const param of node.parameters) {
          const found = bound(name, param.name, file);
          if (found === undefined) return param.initializer ? { node: param.initializer, file, key: `${rel(file)}:${name}` } : null;
          if (found !== false) return found;
        }
      }
      const statements = ts.isSourceFile(node) || ts.isBlock(node) || ts.isModuleBlock(node) ? node.statements : undefined;
      for (const statement of statements ?? []) {
        if (ts.isVariableStatement(statement)) {
          for (const decl of statement.declarationList.declarations) {
            if (ts.isIdentifier(decl.name)) {
              if (decl.name.text === name && decl.initializer) return { node: decl.initializer, file, key: `${rel(file)}:${name}` };
              continue;
            }
            const found = bound(name, decl.name, file);
            if (found !== false) return found ?? null;
          }
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

  // wordOptions lists the word lists (classes, a role) an expression can give: one per branch of its conditionals,
  // so `failed ? 'error-copy' : 'help'` gives ['error-copy'] and ['help'], each checked on its own.
  const wordsIn = (text) => [text.split(/\s+/).filter(Boolean)];
  const product = (a, b) => a.flatMap((left) => b.map((right) => [...left, ...right])).slice(0, 64);
  const wordOptions = (expr, file, depth = 0) => {
    if (!expr || depth > 10) return [[]];
    const node = unwrap(ts.isJsxExpression(expr) ? expr.expression ?? expr : expr);
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) return wordsIn(node.text);
    if (ts.isTemplateExpression(node)) {
      return node.templateSpans.reduce((sum, span) => product(product(sum, wordOptions(span.expression, file, depth + 1)), wordsIn(span.literal.text)), wordsIn(node.head.text));
    }
    if (ts.isConditionalExpression(node)) return [...wordOptions(node.whenTrue, file, depth + 1), ...wordOptions(node.whenFalse, file, depth + 1)];
    if (ts.isBinaryExpression(node)) {
      const op = node.operatorToken.kind;
      if (op === ts.SyntaxKind.AmpersandAmpersandToken) return [[], ...wordOptions(node.right, file, depth + 1)];
      if (op === ts.SyntaxKind.BarBarToken || op === ts.SyntaxKind.QuestionQuestionToken) return [...wordOptions(node.left, file, depth + 1), ...wordOptions(node.right, file, depth + 1)];
      if (op === ts.SyntaxKind.PlusToken) return product(wordOptions(node.left, file, depth + 1), wordOptions(node.right, file, depth + 1));
    }
    if (ts.isIdentifier(node) || ts.isPropertyAccessExpression(node)) {
      const value = resolveValue(node, file);
      return value && value.node !== node && !isFunction(value.node) ? wordOptions(value.node, value.file, depth + 1) : [[]];
    }
    // Anything else (a call that joins classes): every word in it, as if the classes that hide a line were off.
    const words = [];
    const visit = (child) => { if (ts.isStringLiteral(child) || ts.isNoSubstitutionTemplateLiteral(child)) words.push(...wordsIn(child.text)[0]); ts.forEachChild(child, visit); };
    visit(node);
    return [words.filter((word) => !excludedClasses.has(word))];
  };
  const attribute = (opening, name) => opening.attributes.properties.find((attr) => ts.isJsxAttribute(attr) && attr.name.getText() === name);
  const optionsOf = (opening, name, file) => {
    const attr = attribute(opening, name);
    return attr?.initializer ? wordOptions(attr.initializer, file) : [[]];
  };
  const openingOf = (node) => (ts.isJsxElement(node) ? node.openingElement : node);
  const jsxParent = (node) => { for (let at = node.parent; at; at = at.parent) if (ts.isJsxElement(at)) return at; return undefined; };
  const isHelperClass = (name) => helperClasses.has(name) || helperSuffixes.some((suffix) => name.endsWith(suffix));
  const isHelper = (element, file) => {
    const opening = openingOf(element);
    // Only words written in the TSX mark a kept line: data-quiet="state", "consequence" or "value".
    const kept = attribute(opening, 'data-quiet')?.initializer;
    const keptWord = kept && unwrap(ts.isJsxExpression(kept) ? kept.expression ?? kept : kept);
    if (keptWord && ts.isStringLiteral(keptWord) && keptLines.has(keptWord.text)) return false;
    if (optionsOf(opening, 'role', file).every((words) => words.includes('alert'))) return false;
    const shown = optionsOf(opening, 'className', file).filter((words) => !words.some((name) => excludedClasses.has(name)));
    if (shown.some((words) => words.some(isHelperClass))) return true;
    const parent = jsxParent(element);
    return shown.length > 0 && opening.tagName.getText() === 'p' && !!parent && optionsOf(parent.openingElement, 'className', file).some((words) => words.includes('section-heading'));
  };

  const found = new Map();
  const report = (said, file, node) => {
    const text = said.text.replace(/\s+/g, ' ').trim();
    if (text.length <= limit) return;
    const keys = [...new Set(said.keys)];
    const key = keys.length ? keys.join(' + ') : `text:${createHash('sha1').update(text).digest('hex').slice(0, 12)}`;
    const line = sourceOf(file).getLineAndCharacterOfPosition(node.getStart()).line + 1;
    if ((found.get(key)?.length ?? -1) < text.length) found.set(key, { key, length: text.length, text, where: `${rel(file)}:${line}` });
  };
  for (const file of [...files.keys()].filter((path) => path.endsWith('.tsx')).sort()) {
    const visit = (node) => {
      if (ts.isJsxElement(node) && isHelper(node, file)) report(childrenText(node.children, file, 0), file, node);
      if (ts.isJsxAttribute(node) && helperProps.has(node.name.getText()) && node.initializer) {
        report(ts.isStringLiteral(node.initializer) ? { text: node.initializer.text, keys: [] } : textOf(node.initializer.expression, file), file, node);
      }
      ts.forEachChild(node, visit);
    };
    visit(sourceOf(file));
  }
  return [...found.values()];
}
