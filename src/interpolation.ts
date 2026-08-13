import { escapeRegex } from '@grafana/data';

/**
 * Escapes the characters that would otherwise terminate an ERDDAP double-quoted
 * string value.
 *
 * The backslash pass has to run first: escaping quotes first would then double
 * up the backslashes this pass inserts.
 */
function escapeQuoted(value: string): string {
  return value.replace(/\\/g, '\\\\').replace(/"/g, '\\"');
}

/**
 * Formats a dashboard variable value for use inside an ERDDAP constraint.
 *
 * Passed to `TemplateSrv.replace` as its custom formatter, so its shape matches
 * what Grafana calls a `VariableCustomFormatterFn` — that type is not exported
 * from `@grafana/data`, and `replace` only types the argument as
 * `string | Function`, so the signature is declared here instead.
 *
 * A single value is quote-escaped but deliberately *not* regex-escaped, so that
 * `station="$station"` and numeric comparisons such as `depth<$maxdepth` (value
 * `2.5`) both keep working.
 *
 * A multi-value selection becomes a regex alternation — `(A01|B01)` — because
 * ERDDAP tabledap has no OR operator and `=~` against an alternation is its
 * documented substitute. Grafana's own `glob` and `csv` formats produce
 * `{A01,B01}` and `A01,B01`, neither of which means anything to ERDDAP.
 *
 * ERDDAP's structural characters (`&`, `,`, `(`, `)`) are left untouched: the
 * backend's constraint escaper percent-encodes them inside quotes exactly as it
 * would for a hand-typed value.
 *
 * @param value the interpolated variable value, or all selected values
 * @returns the literal text to substitute into the constraint expression
 */
export function formatErddapValue(value: string | string[], _legacyVariableModel?: unknown): string {
  if (!Array.isArray(value)) {
    return escapeQuoted(value);
  }

  if (value.length === 0) {
    return '';
  }

  // A single selection is indistinguishable from the scalar case; wrapping it
  // in an alternation would force the query to use `=~` needlessly.
  if (value.length === 1) {
    return escapeQuoted(value[0]);
  }

  // escapeRegex does not touch `"`, so quote-escaping afterwards cannot corrupt
  // the backslashes it inserts.
  return `(${value.map((entry) => escapeRegex(entry).replace(/"/g, '\\"')).join('|')})`;
}
