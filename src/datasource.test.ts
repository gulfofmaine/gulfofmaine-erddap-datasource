import { DataSourceInstanceSettings, ScopedVars } from '@grafana/data';
import { getTemplateSrv } from '@grafana/runtime';

import { DataSource } from './datasource';
import { formatErddapValue } from './interpolation';
import { ErddapDataSourceOptions, ErddapQuery } from './types';

jest.mock('@grafana/runtime', () => ({
  ...jest.requireActual('@grafana/runtime'),
  getTemplateSrv: jest.fn(),
}));

const getTemplateSrvMock = getTemplateSrv as jest.MockedFunction<typeof getTemplateSrv>;

/** Installs a `replace` that echoes its target, and returns the spy. */
function mockTemplateSrv() {
  const replace = jest.fn((target?: string) => target ?? '');
  getTemplateSrvMock.mockReturnValue({ replace } as unknown as ReturnType<typeof getTemplateSrv>);
  return replace;
}

/**
 * A working stand-in for `TemplateSrv.replace`, used by the end-to-end
 * interpolation tests. Grafana does not publish its implementation, so this
 * reproduces the behaviour the plugin depends on: `$name` and `${name}`
 * references, the `${name:format}` inline override taking precedence over the
 * caller's format, and a function format receiving the raw value.
 */
function interpolate(scopedVars: ScopedVars) {
  return (target?: string, _scopedVars?: ScopedVars, format?: string | Function): string =>
    (target ?? '').replace(/\$\{(\w+)(?::(\w+))?\}|\$(\w+)/g, (match, braced, inlineFormat, bare) => {
      const scoped = scopedVars[braced ?? bare];
      if (!scoped) {
        return match;
      }

      const value = scoped.value as string | string[];
      const effective = inlineFormat ?? format;

      if (typeof effective === 'function') {
        return effective(value);
      }
      if (effective === 'csv') {
        return Array.isArray(value) ? value.join(',') : String(value);
      }
      // Grafana's default (glob) format.
      return Array.isArray(value) ? `{${value.join(',')}}` : String(value);
    });
}

function createDataSource() {
  const instanceSettings = {} as DataSourceInstanceSettings<ErddapDataSourceOptions>;
  return new DataSource(instanceSettings);
}

function query(datasetId?: string, variables?: string): ErddapQuery {
  return { refId: 'A', datasetId, variables };
}

describe('filterQuery', () => {
  const ds = createDataSource();

  it.each`
    description                                | datasetId    | variables        | expected
    ${'undefined datasetId, undefined vars'}   | ${undefined} | ${undefined}     | ${false}
    ${'empty datasetId, empty vars'}           | ${''}        | ${''}            | ${false}
    ${'whitespace-only datasetId and vars'}    | ${'   '}     | ${'   '}         | ${false}
    ${'valid datasetId, undefined vars'}       | ${'M01'}     | ${undefined}     | ${false}
    ${'undefined datasetId, valid vars'}       | ${undefined} | ${'temperature'} | ${false}
    ${'valid datasetId, empty vars'}           | ${'M01'}     | ${''}            | ${false}
    ${'valid datasetId, whitespace-only vars'} | ${'M01'}     | ${'   '}         | ${false}
    ${'whitespace-only datasetId, valid vars'} | ${'   '}     | ${'temperature'} | ${false}
    ${'valid datasetId, valid vars'}           | ${'M01'}     | ${'temperature'} | ${true}
  `('returns $expected when $description', ({ datasetId, variables, expected }) => {
    expect(ds.filterQuery(query(datasetId, variables))).toBe(expected);
  });
});

describe('discovery resource calls', () => {
  let getResource: jest.SpyInstance;

  beforeEach(() => {
    getResource = jest.spyOn(DataSource.prototype, 'getResource').mockResolvedValue({});
  });

  afterEach(() => {
    getResource.mockRestore();
  });

  it('searchDatasets calls the datasets resource with the search text and default limit', async () => {
    const ds = createDataSource();
    await ds.searchDatasets('temp');
    expect(getResource).toHaveBeenCalledWith('datasets', { q: 'temp', limit: 100 });
  });

  it('searchDatasets forwards an explicit limit', async () => {
    const ds = createDataSource();
    await ds.searchDatasets('temp', 5);
    expect(getResource).toHaveBeenCalledWith('datasets', { q: 'temp', limit: 5 });
  });

  it('getVariables calls the variables resource with the dataset id', async () => {
    const ds = createDataSource();
    await ds.getVariables('M01');
    expect(getResource).toHaveBeenCalledWith('variables', { datasetId: 'M01' });
  });

  it('propagates rejections from getResource', async () => {
    getResource.mockRejectedValue(new Error('boom'));
    const ds = createDataSource();
    await expect(ds.getVariables('M01')).rejects.toThrow('boom');
  });
});

describe('applyTemplateVariables', () => {
  const scopedVars: ScopedVars = {};

  it('runs constraints through the ERDDAP formatter', () => {
    const replace = mockTemplateSrv();
    const ds = createDataSource();

    ds.applyTemplateVariables({ refId: 'A', constraints: 'station="$station"' }, scopedVars);

    expect(replace).toHaveBeenCalledWith('station="$station"', scopedVars, formatErddapValue);
  });

  it('runs variables through the csv format', () => {
    const replace = mockTemplateSrv();
    const ds = createDataSource();

    ds.applyTemplateVariables({ refId: 'A', variables: '$vars' }, scopedVars);

    expect(replace).toHaveBeenCalledWith('$vars', scopedVars, 'csv');
  });

  it('leaves datasetId uninterpolated', () => {
    const replace = mockTemplateSrv();
    const ds = createDataSource();

    const result = ds.applyTemplateVariables({ refId: 'A', datasetId: '$dataset' }, scopedVars);

    expect(result.datasetId).toBe('$dataset');
    expect(replace).not.toHaveBeenCalled();
  });

  it.each`
    field            | query
    ${'constraints'} | ${{ refId: 'A', variables: 'temperature' }}
    ${'variables'}   | ${{ refId: 'A', constraints: 'depth<2' }}
  `('leaves an absent $field absent rather than empty', ({ field, query }) => {
    mockTemplateSrv();
    const ds = createDataSource();

    const result: ErddapQuery = ds.applyTemplateVariables(query, scopedVars);

    expect(result[field as 'constraints' | 'variables']).toBeUndefined();
  });

  it('preserves the other query fields', () => {
    mockTemplateSrv();
    const ds = createDataSource();

    const result = ds.applyTemplateVariables(
      { refId: 'B', hide: true, datasetId: 'M01', variables: 'temperature', constraints: 'depth<2' },
      scopedVars
    );

    expect(result).toEqual({
      refId: 'B',
      hide: true,
      datasetId: 'M01',
      variables: 'temperature',
      constraints: 'depth<2',
    });
  });

  it('turns a multi-value variable into a regex alternation end to end', () => {
    const vars: ScopedVars = {
      station: { text: 'A01 + B01', value: ['A01', 'B01'] },
      fields: { text: 'temperature + salinity', value: ['temperature', 'salinity'] },
    };
    getTemplateSrvMock.mockReturnValue({ replace: interpolate(vars) } as unknown as ReturnType<typeof getTemplateSrv>);
    const ds = createDataSource();

    const result = ds.applyTemplateVariables(
      { refId: 'A', datasetId: 'M01', variables: '$fields', constraints: 'station=~"$station"&depth<2' },
      vars
    );

    expect(result.constraints).toBe('station=~"(A01|B01)"&depth<2');
    expect(result.variables).toBe('temperature,salinity');
  });

  it('quotes a single-valued variable without regex escaping it', () => {
    const vars: ScopedVars = { maxdepth: { text: '2.5', value: '2.5' } };
    getTemplateSrvMock.mockReturnValue({ replace: interpolate(vars) } as unknown as ReturnType<typeof getTemplateSrv>);
    const ds = createDataSource();

    const result = ds.applyTemplateVariables({ refId: 'A', constraints: 'depth<$maxdepth' }, vars);

    expect(result.constraints).toBe('depth<2.5');
  });

  it('lets an inline format override the plugin formatter', () => {
    const vars: ScopedVars = { station: { text: 'A01 + B01', value: ['A01', 'B01'] } };
    getTemplateSrvMock.mockReturnValue({ replace: interpolate(vars) } as unknown as ReturnType<typeof getTemplateSrv>);
    const ds = createDataSource();

    const result = ds.applyTemplateVariables({ refId: 'A', constraints: '${station:csv}' }, vars);

    expect(result.constraints).toBe('A01,B01');
  });
});
