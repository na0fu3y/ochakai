// An OKF document as text, and the edits the page makes to it: a
// template for a new one, and one scalar frontmatter key set.
//
// Pure, and deliberately so. This is the module that rewrites what
// somebody wrote, so it is the module that most needs to be held to
// examples — and it can be, because it takes a string and returns one.

// Templates for a new entry, one per recommended type (design doc 0038),
// so a create starts from a shape rather than an empty textarea.
//
// The two halves are separated because they travel differently. REQUIRED
// is what the write path refuses the type without — an Attested
// Computation with no runtime is rejected by the server — so a type
// change carries it into a document somebody is already writing. SEED is
// illustrative: a sample parameter, a resource spelled out so a fresh
// template shows the shape. Pasting an example `year` parameter into a
// half-written document would be putting words in its writer's mouth,
// so seeds only ever open an empty one.
export const TYPE_REQUIRED = {
  'Attested Computation': 'runtime: bigquery\n',
};
export const TYPE_SEED = {
  'Attested Computation': 'parameters:\n    - name: year\n      type: integer\n      required: true\n',
  'BigQuery Dataset': 'resource: bigquery://project/dataset\n',
  'BigQuery Table': 'resource: bigquery://project/dataset/table\n',
  Reference: 'resource: https://example.com/the-original\n',
};

export function templateDocument(type, title) {
  const extra = (TYPE_REQUIRED[type] || '') + (TYPE_SEED[type] || '');
  return '---\ntype: ' + type + '\n'
    + (title ? 'title: ' + JSON.stringify(title) + '\n' : '')
    + 'description: ""\nstatus: draft\n' + extra
    + '---\n\n';
}

// withFrontmatterKey returns doc with one scalar frontmatter key set: the
// line replaced when the document has one, and appended to the
// frontmatter when it does not. A writer who named no value leaves the
// key absent, and the server does not fill it in (design doc 0046 §3.9) —
// so an edit that only replaced would report success and change nothing.
//
// Still a line edit rather than a parser (design doc 0044 §2.1 refuses
// the parser; this refuses to need one), which is why it is only ever
// asked for scalars — a repeating structure is what would need the
// parser 0072 §3.1 turned down. The alternative, rebuilding a document
// from the fields the page holds, is what erased data before: the page
// holds the projection, and a document assembled from it would drop
// everything the projection does not carry — the citations, the
// contract, the body (design doc 0035 §4 is the same bug with fewer
// fields).
//
// One implementation because there are two callers now, and two copies
// of an append path only one of which gets a fix is how the type
// vocabulary drifted before.
export function withFrontmatterKey(doc, key, value) {
  const line = key + ': ' + value;
  const re = new RegExp('^' + key + ':.*$', 'm');
  if (re.test(doc)) return doc.replace(re, line);
  // Append inside the frontmatter block: after the opening delimiter's
  // line, before the closing one. A document with no frontmatter is not
  // one this UI can write, so it is handed back untouched and the PUT
  // fails with the server's own message.
  const m = doc.match(/^(---\r?\n[\s\S]*?)(---\r?\n[\s\S]*)$/);
  return m ? m[1] + line + '\n' + m[2] : doc;
}

export function withStatus(doc, status) {
  return withFrontmatterKey(doc, 'status', status);
}

// withType returns doc with its type line set, plus any key the new type
// is refused without and the document does not already name. Setting the
// type alone would hand back a document the server rejects — the one
// place a type is held to a schema is an Attested Computation's runtime
// (design doc 0036 §5) — and reporting success for that is worse than
// doing nothing, which is what this dropdown used to do.
//
// A key the writer already named is theirs, not the template's to
// overwrite. Keys the *previous* type wanted are left where they stand:
// no per-type rule rejects them, and removing a line its writer wrote is
// not this page's to do (design doc 0044 §2.1 is the same posture).
export function withType(doc, type) {
  let out = withFrontmatterKey(doc, 'type', type);
  for (const req of (TYPE_REQUIRED[type] || '').split('\n')) {
    const i = req.indexOf(':');
    if (i < 0) continue;
    const key = req.slice(0, i);
    if (!new RegExp('^' + key + ':', 'm').test(out)) {
      out = withFrontmatterKey(out, key, req.slice(i + 2));
    }
  }
  return out;
}
