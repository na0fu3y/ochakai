// The vocabularies the server stores, as the page spells them: OKF's
// types (design doc 0023) and its lifecycle values (SPEC §5.4).

// Keys are the OKF type vocabulary, which is what the server stores
// (design doc 0023). Lookup is case-insensitive to match the server's
// filter matching — the stored spelling is whatever the writer used.
export const TYPE_ICONS = { 'Metric': '📊', 'Attested Computation': '🧮', 'Skill': '🛠️', 'Insight': '💡', 'Policy': '⚖️', 'Glossary Term': '📖', 'BigQuery Dataset': '🗂️', 'BigQuery Table': '🗃️', 'Reference': '🔖' };
export const ICON_BY_LOWER = new Map(Object.entries(TYPE_ICONS).map(([k, v]) => [k.toLowerCase(), v]));
export const icon = t => ICON_BY_LOWER.get(String(t ?? '').trim().toLowerCase()) || '📄';
export const KNOWN_TYPES = Object.keys(TYPE_ICONS);

// groupByType splits a directory's concepts into one group per type, for
// the directory page: a team's prefix then reads as its hub — metrics
// together, terms together. Recommended types come in their teaching
// order (definition → computation → interpretation → catalog →
// reference) under their recommended spelling, then free types by name;
// types match case-insensitively, as the server's filter does, and a
// free type is labelled the way its first concept spells it. Order
// within a group is the order given. The generated index.md stays flat:
// this is how the page reads it, not what the bundle says.
export function groupByType(concepts) {
  const known = new Map(KNOWN_TYPES.map((t, i) => [t.toLowerCase(), i]));
  const groups = new Map();
  for (const c of concepts) {
    const label = String(c.type ?? '').trim();
    const key = label.toLowerCase();
    if (!groups.has(key)) {
      const rank = known.get(key);
      groups.set(key, { type: rank === undefined ? label : KNOWN_TYPES[rank], rank: rank ?? KNOWN_TYPES.length, concepts: [] });
    }
    groups.get(key).concepts.push(c);
  }
  return [...groups.values()]
    .sort((a, b) => a.rank - b.rank || a.type.localeCompare(b.type))
    .map(({ type, concepts }) => ({ type, concepts }));
}
// OKF's lifecycle vocabulary (SPEC §5.4). Whether anyone confirmed an
// entry, and whether anyone turned it down, are ledgers rather than
// statuses and have their own actions (design doc 0043 §§3.2-3.3).
export const STATUSES = ['draft', 'stable', 'deprecated'];
