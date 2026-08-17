import { formatErddapValue } from './interpolation';

describe('formatErddapValue', () => {
  // The `input`/`expected` columns hold the *runtime* strings; the JS escaping
  // in the literals below is doubled as usual, so the comments spell out what
  // the values actually are.
  it.each`
    description                                    | input                    | expected
    ${'a plain single value'}                      | ${'A01'}                 | ${'A01'}
    ${'an empty string'}                           | ${''}                    | ${''}
    ${'a single value containing a quote'}         | ${'say "hi"'}            | ${'say \\"hi\\"'}
    ${'a single value containing a backslash'}     | ${'a\\b'}                | ${'a\\\\b'}
    ${'a single value with a backslash-quote'}     | ${'a\\"b'}               | ${'a\\\\\\"b'}
    ${'a single value with regex metacharacters'}  | ${'2.5'}                 | ${'2.5'}
    ${'a single value with a pipe'}                | ${'A|B'}                 | ${'A|B'}
    ${'a single-element array'}                    | ${['A01']}               | ${'A01'}
    ${'a single-element array needing escaping'}   | ${['a"b']}               | ${'a\\"b'}
    ${'an empty array'}                            | ${[]}                    | ${''}
    ${'a multi-value selection'}                   | ${['A01', 'B01']}        | ${'(A01|B01)'}
    ${'a multi-value with regex metacharacters'}   | ${['2.5', '3.5']}        | ${'(2\\.5|3\\.5)'}
    ${'a multi-value with quotes'}                 | ${['a"b', 'c']}          | ${'(a\\"b|c)'}
    ${'a multi-value with a backslash'}            | ${['a\\b', 'c']}         | ${'(a\\\\b|c)'}
    ${'a multi-value with ERDDAP structure chars'} | ${['A&B', 'C,D']}        | ${'(A&B|C,D)'}
    ${'a multi-value with three entries'}          | ${['A01', 'B01', 'C01']} | ${'(A01|B01|C01)'}
  `('formats $description', ({ input, expected }) => {
    expect(formatErddapValue(input)).toBe(expected);
  });

  it('leaves a single value un-regex-escaped so numeric comparisons keep working', () => {
    // `depth<$maxdepth` must stay a usable numeric comparison.
    expect(formatErddapValue('2.5')).not.toContain('\\');
  });

  it('ignores the legacy variable model Grafana passes as a second argument', () => {
    expect(formatErddapValue('A01', { name: 'station' })).toBe('A01');
  });
});
