import { normalizeBaseUrl, validateBaseUrl } from './validation';

describe('validateBaseUrl', () => {
  it.each`
    description                | value                                    | expected
    ${'empty string'}          | ${''}                                    | ${'ERDDAP URL is required'}
    ${'whitespace only'}       | ${'   '}                                 | ${'ERDDAP URL is required'}
    ${'unparseable text'}      | ${'not a url'}                           | ${'Enter a full URL, for example https://data.neracoos.org/erddap'}
    ${'missing scheme'}        | ${'data.neracoos.org/erddap'}            | ${'Enter a full URL, for example https://data.neracoos.org/erddap'}
    ${'non-http scheme'}       | ${'ftp://x'}                             | ${'URL must start with http:// or https://'}
    ${'bare http host'}        | ${'http://x'}                            | ${undefined}
    ${'https erddap url'}      | ${'https://data.neracoos.org/erddap'}    | ${undefined}
    ${'https with trailing /'} | ${'https://data.neracoos.org/erddap/'}   | ${undefined}
    ${'valid with whitespace'} | ${'  https://data.neracoos.org/erddap '} | ${undefined}
  `('returns $expected for $description', ({ value, expected }) => {
    expect(validateBaseUrl(value)).toBe(expected);
  });
});

describe('normalizeBaseUrl', () => {
  it.each`
    description                     | value                                    | expected
    ${'leaves canonical url alone'} | ${'https://data.neracoos.org/erddap'}    | ${'https://data.neracoos.org/erddap'}
    ${'trims whitespace'}           | ${'  https://data.neracoos.org/erddap '} | ${'https://data.neracoos.org/erddap'}
    ${'strips one trailing slash'}  | ${'https://data.neracoos.org/erddap/'}   | ${'https://data.neracoos.org/erddap'}
    ${'strips many slashes'}        | ${'https://data.neracoos.org/erddap///'} | ${'https://data.neracoos.org/erddap'}
    ${'trims then strips'}          | ${'  https://x/erddap//  '}              | ${'https://x/erddap'}
    ${'handles empty string'}       | ${''}                                    | ${''}
    ${'handles whitespace only'}    | ${'   '}                                 | ${''}
  `('$description', ({ value, expected }) => {
    expect(normalizeBaseUrl(value)).toBe(expected);
  });

  it('is idempotent', () => {
    const once = normalizeBaseUrl('  https://data.neracoos.org/erddap//  ');
    expect(normalizeBaseUrl(once)).toBe(once);
  });
});
