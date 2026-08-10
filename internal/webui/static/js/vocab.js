// The vocabularies the server stores, as the page spells them: OKF's
// types (design doc 0023) and its lifecycle values (SPEC §5.4).

// Keys are the OKF type vocabulary, which is what the server stores
// (design doc 0023). Lookup is case-insensitive to match the server's
// filter matching — the stored spelling is whatever the writer used.
export const TYPE_ICONS = { 'Metric': '📊', 'Attested Computation': '🧮', 'Skill': '🛠️', 'Insight': '💡', 'Policy': '⚖️', 'Glossary Term': '📖', 'BigQuery Dataset': '🗂️', 'BigQuery Table': '🗃️', 'Reference': '🔖' };
export const ICON_BY_LOWER = new Map(Object.entries(TYPE_ICONS).map(([k, v]) => [k.toLowerCase(), v]));
export const icon = t => ICON_BY_LOWER.get(String(t ?? '').trim().toLowerCase()) || '📄';
export const KNOWN_TYPES = Object.keys(TYPE_ICONS);
// OKF's lifecycle vocabulary (SPEC §5.4). Whether anyone confirmed an
// entry, and whether anyone turned it down, are ledgers rather than
// statuses and have their own actions (design doc 0043 §§3.2-3.3).
export const STATUSES = ['draft', 'stable', 'deprecated'];
