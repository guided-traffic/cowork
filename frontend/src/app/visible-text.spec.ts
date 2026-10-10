import {
  AST,
  ASTWithSource,
  Interpolation,
  KeyedRead,
  LiteralPrimitive,
  parseTemplate,
  RecursiveAstVisitor,
  SafeKeyedRead,
  TemplateLiteral,
  TmplAstBoundAttribute,
  TmplAstBoundText,
  TmplAstElement,
  TmplAstRecursiveVisitor,
  TmplAstText,
  TmplAstTextAttribute,
  tmplAstVisitAll,
} from '@angular/compiler';
import type * as TypeScript from 'typescript';

/**
 * What the UI shows calls a tenant a team (docs/adr/0005 D1): the database keeps the word, a
 * person never reads it. This reads every component template — the `.html` files and the inline
 * `template`s — and every string of the code, the texts, labels and options a page builds, and
 * fails on the word wherever a person could read it, alone or in a sentence, in any case.
 */
const forbidden = /\btenants?\b/i;

/** The word alone, in any case: a label, an option or a value a list shows as it is. */
const lone = /^\s*tenants?\s*$/i;

/** A text that says the word on purpose, where it is, and why. */
interface Allowed {
  /** The file under `src/app`. */
  file: string;
  /** In code, the declaration the string is in; in a template, `text` or the attribute. */
  owner: string;
  text: string;
  why: string;
}

/**
 * Where the word stays on purpose: a value of the API the server requires as it is, which the page
 * never shows. An entry that no longer matches fails the test, so that the list does not outlive
 * its reason.
 */
const shownOnPurpose: readonly Allowed[] = [
  {
    file: 'features/tenant/audit.ts',
    owner: 'storedEntityTypes',
    text: 'tenant',
    why:
      'the entity type the audit record stores for a team, which the filter asks for when a ' +
      'person types team; a row shows it as team (docs/adr/0005 D1, docs/adr/0026 D6)',
  },
];

/** The attributes and inputs whose value a person reads or hears, or picks from. */
const visibleAttributes = new Set(
  [
    'alt',
    'aria-description',
    'aria-label',
    'aria-placeholder',
    'aria-roledescription',
    'aria-valuetext',
    'ariaLabel',
    'currentPageReportTemplate',
    'emptyFilterMessage',
    'emptyMessage',
    'header',
    'label',
    'options',
    'placeholder',
    'prefix',
    'pTooltip',
    'suffix',
    'suggestions',
    'title',
    'what',
  ].map((name) => name.toLowerCase()),
);

/** The inputs whose value is no text in the field: the rest show what `value` holds. */
const valueNotShown = new Set(['checkbox', 'color', 'file', 'hidden', 'image', 'radio', 'range']);

/** The string parts of an expression a person reads: its literals, but no key of an index. */
class Literals extends RecursiveAstVisitor {
  readonly found: string[] = [];

  override visitLiteralPrimitive(ast: LiteralPrimitive): void {
    if (typeof ast.value === 'string') {
      this.found.push(ast.value);
    }
  }

  override visitKeyedRead(ast: KeyedRead, context: unknown): void {
    ast.receiver.visit(this, context);
  }

  override visitSafeKeyedRead(ast: SafeKeyedRead, context: unknown): void {
    ast.receiver.visit(this, context);
  }

  override visitInterpolation(ast: Interpolation, context: unknown): void {
    this.found.push(...ast.strings);
    this.visitAll(ast.expressions, context);
  }

  override visitTemplateLiteral(ast: TemplateLiteral, context: unknown): void {
    this.found.push(...ast.elements.map((element) => element.text));
    this.visitAll(ast.expressions, context);
  }
}

function literalsOf(value: AST): string[] {
  const visitor = new Literals();
  (value instanceof ASTWithSource ? value.ast : value).visit(visitor);
  return visitor.found;
}

/** A text a person could read, where it is. */
interface Shown {
  line: number;
  owner: string;
  text: string;
}

/** The texts of a template a person reads: its text, its visible attributes and their literals. */
class VisibleText extends TmplAstRecursiveVisitor {
  readonly shown: Shown[] = [];
  /** Whether the element whose attributes are visited shows its `value`: an option, a field. */
  private showsValue = false;

  private add(line: number, owner: string, text: string): void {
    this.shown.push({ line: line + 1, owner, text: text.replace(/\s+/g, ' ').trim() });
  }

  private visible(name: string): boolean {
    return visibleAttributes.has(name.toLowerCase()) || (name === 'value' && this.showsValue);
  }

  override visitElement(element: TmplAstElement): void {
    const held = this.showsValue;
    const type = element.attributes.find((attribute) => attribute.name === 'type')?.value ?? '';
    this.showsValue =
      element.name === 'option' || (element.name === 'input' && !valueNotShown.has(type));
    super.visitElement(element);
    this.showsValue = held;
  }

  override visitText(text: TmplAstText): void {
    this.add(text.sourceSpan.start.line, 'text', text.value);
  }

  override visitBoundText(text: TmplAstBoundText): void {
    for (const part of literalsOf(text.value)) {
      this.add(text.sourceSpan.start.line, 'text', part);
    }
  }

  override visitTextAttribute(attribute: TmplAstTextAttribute): void {
    if (this.visible(attribute.name)) {
      this.add(attribute.sourceSpan.start.line, attribute.name, attribute.value);
    }
  }

  override visitBoundAttribute(attribute: TmplAstBoundAttribute): void {
    if (this.visible(attribute.name)) {
      for (const part of literalsOf(attribute.value)) {
        this.add(attribute.sourceSpan.start.line, `[${attribute.name}]`, part);
      }
    }
  }
}

/** The texts of one template that say the word, each with its line in the file. */
function visibleWords(template: string, lineOffset = 0): Shown[] {
  const parsed = parseTemplate(template, 'template.html', { preserveWhitespaces: false });
  if (parsed.errors && parsed.errors.length > 0) {
    throw new Error(`the template does not parse: ${parsed.errors.join('; ')}`);
  }
  const visitor = new VisibleText();
  tmplAstVisitAll(visitor, parsed.nodes);
  return visitor.shown
    .filter((shown) => forbidden.test(shown.text))
    .map((shown) => ({ ...shown, line: shown.line + lineOffset }));
}

/** The inline templates of a component file, each with the line it starts on. */
function inlineTemplates(source: string): { template: string; line: number }[] {
  const found: { template: string; line: number }[] = [];
  for (const match of source.matchAll(/\btemplate:\s*`/g)) {
    const start = (match.index ?? 0) + match[0].length;
    found.push({
      template: source.slice(start, source.indexOf('`', start)),
      line: source.slice(0, start).split('\n').length - 1,
    });
  }
  return found;
}

/** The properties of a component that hold no text a person reads, or that are read elsewhere. */
const notText = new Set(['selector', 'styles', 'styleUrl', 'styleUrls', 'template', 'templateUrl']);

/** The name a declaration or a property goes by, where it has one written out. */
function nameOf(ts: typeof TypeScript, node: TypeScript.Node): string | undefined {
  const name = (node as { name?: TypeScript.Node }).name;
  return name && (ts.isIdentifier(name) || ts.isStringLiteral(name)) ? name.text : undefined;
}

/** The declaration a string is in, by its name: a constant, a member, a function. */
function ownerOf(ts: typeof TypeScript, node: TypeScript.Node): string {
  for (let at = node.parent; at; at = at.parent) {
    if (
      ts.isVariableDeclaration(at) ||
      ts.isPropertyDeclaration(at) ||
      ts.isMethodDeclaration(at) ||
      ts.isFunctionDeclaration(at) ||
      ts.isGetAccessorDeclaration(at) ||
      ts.isClassDeclaration(at)
    ) {
      const name = nameOf(ts, at);
      if (name) {
        return name;
      }
    }
  }
  return '(module)';
}

/**
 * Whether a string of the code stands where a name stands, never shown: in a type, as a key — of
 * an index, of a property, after `in` —, compared or matched against a value, or as a test id.
 */
function standsForAName(ts: typeof TypeScript, node: TypeScript.Node): boolean {
  for (let at = node.parent; at; at = at.parent) {
    if (ts.isTypeNode(at)) {
      return true;
    }
  }
  const parent = node.parent;
  const compares = [
    ts.SyntaxKind.EqualsEqualsEqualsToken,
    ts.SyntaxKind.ExclamationEqualsEqualsToken,
    ts.SyntaxKind.EqualsEqualsToken,
    ts.SyntaxKind.ExclamationEqualsToken,
    ts.SyntaxKind.InKeyword,
  ];
  const testId = /^(data-)?test-?id$/i;
  const property = ts.isPropertyAssignment(parent) ? nameOf(ts, parent) : undefined;
  return (
    (ts.isElementAccessExpression(parent) && parent.argumentExpression === node) ||
    (parent as { name?: TypeScript.Node }).name === node ||
    (ts.isBinaryExpression(parent) && compares.includes(parent.operatorToken.kind)) ||
    (ts.isCaseClause(parent) && parent.expression === node) ||
    testId.test(property ?? '') ||
    testId.test(ownerOf(ts, node))
  );
}

/**
 * The strings of a source file that say the word where a person could read it, each with its
 * line and declaration: a sentence anywhere, and the word alone wherever it stands for no name.
 */
function codeWords(ts: typeof TypeScript, fileName: string, source: string): Shown[] {
  const file = ts.createSourceFile(fileName, source, ts.ScriptTarget.Latest, true);
  const shown: Shown[] = [];
  const check = (node: TypeScript.Node, text: string) => {
    const sentence = forbidden.test(text) && /\s/.test(text.trim());
    if (sentence || (lone.test(text) && !standsForAName(ts, node))) {
      shown.push({
        line: file.getLineAndCharacterOfPosition(node.getStart(file)).line + 1,
        owner: ownerOf(ts, node),
        text: text.replace(/\s+/g, ' ').trim(),
      });
    }
  };
  const visit = (node: TypeScript.Node): void => {
    if (ts.isImportDeclaration(node) || ts.isExportDeclaration(node)) {
      return;
    }
    if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword) {
      return;
    }
    if (ts.isPropertyAssignment(node) && notText.has(nameOf(ts, node) ?? '')) {
      return;
    }
    if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) {
      check(node, node.text);
    } else if (ts.isTemplateExpression(node)) {
      for (const part of [node.head, ...node.templateSpans.map((span) => span.literal)]) {
        check(part, part.text);
      }
    }
    ts.forEachChild(node, visit);
  };
  visit(file);
  return shown;
}

/** What the spec needs of `node:fs`. */
interface FileSystem {
  existsSync(path: string): boolean;
  readdirSync(path: string, options: { recursive: true; encoding: 'utf8' }): string[];
  readFileSync(path: string, encoding: 'utf8'): string;
}

/**
 * A module of Node, loaded when the test runs: the specs are built for the browser, without the
 * types of Node, and run in Node, which has them.
 */
async function nodeModule<T>(name: string): Promise<T> {
  const loaded = (await import(/* @vite-ignore */ name)) as T & { default?: T };
  return loaded.default ?? loaded;
}

/** A text found, in which file and whether in a template or in the code. */
interface Found extends Shown {
  file: string;
  in: 'template' | 'code';
}

/** What reading the application's sources found, as `ng test` sees them from `frontend/`. */
interface Scan {
  templates: number;
  codeFiles: number;
  found: Found[];
}

let scanned: Promise<Scan> | undefined;

async function readSources(): Promise<Scan> {
  const fs = await nodeModule<FileSystem>('node:fs');
  const ts = await nodeModule<typeof TypeScript>('typescript');
  const root = `${(globalThis as unknown as { process: { cwd(): string } }).process.cwd()}/src/app`;
  if (!fs.existsSync(root)) {
    throw new Error(`no ${root}: run the unit tests from frontend/ (make frontend-test)`);
  }
  const scan: Scan = { templates: 0, codeFiles: 0, found: [] };
  const files = fs
    .readdirSync(root, { recursive: true, encoding: 'utf8' })
    .filter((path) => !path.startsWith('api/') && !path.endsWith('.spec.ts'))
    .filter((path) => path.endsWith('.html') || path.endsWith('.ts'))
    .sort();
  for (const file of files) {
    const source = fs.readFileSync(`${root}/${file}`, 'utf8');
    const templates = file.endsWith('.html')
      ? [{ template: source, line: 0 }]
      : inlineTemplates(source);
    scan.templates += templates.length;
    for (const { template, line } of templates) {
      scan.found.push(
        ...visibleWords(template, line).map((each) => ({ ...each, file, in: 'template' as const })),
      );
    }
    if (file.endsWith('.ts')) {
      scan.codeFiles++;
      scan.found.push(
        ...codeWords(ts, file, source).map((each) => ({ ...each, file, in: 'code' as const })),
      );
    }
  }
  return scan;
}

/** The sources, read once for the tests below. */
function sources(): Promise<Scan> {
  scanned ??= readSources();
  return scanned;
}

const allowed = (found: Found) =>
  shownOnPurpose.some(
    (entry) =>
      entry.file === found.file && entry.owner === found.owner && entry.text === found.text,
  );

const reported = (found: Found[]) =>
  found
    .filter((each) => !allowed(each))
    .map((each) => `${each.file}:${each.line} ${each.owner}: ${each.text}`);

describe('what the UI shows (docs/adr/0005 D1)', () => {
  it('finds the word in the text, the visible attributes and their literals of a template, alone or not, in any case', () => {
    const words = visibleWords(
      [
        '<h1>Teams</h1>',
        "<p-select placeholder='Choose a tenant' data-testid='tenant-switch' />",
        "<span [pTooltip]=\"shared ? 'With the Tenant' : 'Mine'\">{{ errors()['tenant'] }}</span>",
        "<a [routerLink]=\"['/t', tenant]\" [attr.aria-label]=\"'Open ' + name\">the tenant's page</a>",
        '@if (tenant()) { <p>{{ count }} tenants</p> }',
        '<p-selectbutton [options]="[\'project\', \'tenant\']" [tenant]="tenant()" />',
        '<datalist id="kinds"><option value="tenant"></option></datalist><span>TENANT</span>',
        '<input type="hidden" value="tenant" /><input type="submit" value="Tenants" />',
      ].join('\n'),
    );

    expect(words.map((word) => `${word.line} ${word.owner}: ${word.text}`)).toEqual([
      '2 placeholder: Choose a tenant',
      '3 [pTooltip]: With the Tenant',
      "4 text: the tenant's page",
      '5 text: tenants',
      '6 [options]: tenant',
      '7 value: tenant',
      '7 text: TENANT',
      '8 value: Tenants',
    ]);
  });

  it('finds the word in a string of the code a person could read, and not where it stands for a name', async () => {
    const ts = await nodeModule<typeof TypeScript>('typescript');
    const source = [
      "import { x } from './features/tenant/x';",
      "const message = 'Your turns in this tenant are stopped.';",
      "const label = 'Tenant';",
      "const key = errors()['tenant'];",
      'const path = `/api/v1/tenants/${tenant}/events`;',
      'const toast = `${name} is no longer a member of this tenant.`;',
      "@Component({ selector: 'app-tenant-board', template: `<p>A tenant</p>` })",
      'class Board {',
      "  readonly groups: Group[] = ['project', 'ticket', 'tenant'];",
      "  readonly testId = input('tenant');",
      '}',
      "type Group = 'project' | 'ticket' | 'tenant';",
      "const shown = scope === 'tenant' ? 'here' : 'everywhere';",
      "messages.add({ severity: 'info', summary: 'tenants', detail: 'Done.' });",
      "const options = [{ label: 'team', value: 'tenant' }];",
      "const host = { 'data-testid': 'tenant', tenant: 'acme' };",
      "const known = 'tenant' in payload;",
      "const keys = { tenant: 'tenant', 'tenants': 'x' };",
    ].join('\n');

    expect(
      codeWords(ts, 'x.ts', source).map((word) => `${word.line} ${word.owner}: ${word.text}`),
    ).toEqual([
      '2 message: Your turns in this tenant are stopped.',
      '3 label: Tenant',
      '6 toast: is no longer a member of this tenant.',
      '9 groups: tenant',
      '14 (module): tenants',
      '15 options: tenant',
      '18 keys: tenant',
    ]);
  });

  it('says team, never tenant, in any template', async () => {
    const scan = await sources();

    expect(scan.templates).toBeGreaterThan(50);
    expect(reported(scan.found.filter((each) => each.in === 'template'))).toEqual([]);
  });

  it('says team, never tenant, in any string of the code a person could read', async () => {
    const scan = await sources();

    expect(scan.codeFiles).toBeGreaterThan(100);
    expect(reported(scan.found.filter((each) => each.in === 'code'))).toEqual([]);
  });

  it('keeps on the list of texts shown on purpose only what is still there, each with its reason', async () => {
    const scan = await sources();
    const stale = shownOnPurpose.filter(
      (entry) =>
        !scan.found.some(
          (each) =>
            entry.file === each.file && entry.owner === each.owner && entry.text === each.text,
        ),
    );

    expect(stale).toEqual([]);
    expect(shownOnPurpose.every((entry) => entry.why.trim().length > 0)).toBe(true);
  });
});
